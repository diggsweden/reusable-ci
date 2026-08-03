// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/openpgp"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

// ArtifactSignatureInput drives `reusable-ci validate artifact-
// signature`. The consumer invocation is meant to read like:
//
//	reusable-ci validate artifact-signature --artifact app.tgz \
//	    --cert-identity-regexp '^https://github\.com/<owner>/<repo>/'
//
// — with the method (gpg | sigstore-or-kms) inferred from which
// sidecar files are present (.asc vs .bundle), unless overridden
// explicitly. For .bundle sidecars the inner trust model (keyless
// vs KMS) is decided by whether --key or --cert-identity-regexp is
// supplied.
type ArtifactSignatureInput struct {
	// Artifact is the file whose signature is verified. Required.
	Artifact string

	// SignaturePath overrides the sidecar lookup. Empty means
	// "look next to Artifact for .bundle (cosign) or .asc (gpg)".
	SignaturePath string

	// Method overrides the auto-detection result. Empty enables
	// auto-detect from the sidecar files; an explicit value short-
	// circuits the detection (operator knows what was used).
	Method domainrelease.SignMethod

	// CertIdentityRegexp + CertOIDCIssuer constrain who could have
	// produced the keyless signature. Both required for sigstore.
	// Examples:
	//   identity: "^https://github.com/examplescope/myapp/\\.github/workflows/release\\.yml@refs/tags/v.+$"
	//   issuer:   "https://token.actions.githubusercontent.com"
	CertIdentityRegexp string
	CertOIDCIssuer     string

	// KeyRef is the cosign --key for kms verification: KMS URI
	// (awskms://, hashivault://), PKCS#11 URI, or a local pubkey
	// file path.
	KeyRef string

	// PublicKey is the armored GPG public key for gpg verification.
	// Required when method == gpg.
	PublicKey []byte
}

// VerifyArtifactSignature inspects the on-disk sidecar layout,
// resolves the signing method (or honours an explicit override),
// and dispatches to the matching verifier. Returns nil on success;
// errs.ErrPermissionDenied wrapping the underlying verify error on
// signature mismatch.
//
// The cosignVerifier slice is the contract: production passes
// *cosign.Adapter; tests pass an in-process fake.
func VerifyArtifactSignature(
	ctx context.Context, cosignVerifier cosignBlobVerifier, out io.Writer, in ArtifactSignatureInput,
) error {
	if in.Artifact == "" {
		return fmt.Errorf("validate artifact-signature: --artifact is required: %w", errs.ErrMissingInput)
	}

	resolved, err := resolveSignatureLayout(in)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "Verifying %s (method=%s, signature=%s)\n", in.Artifact, resolved.method, resolved.signaturePath)

	switch resolved.method {
	case domainrelease.SignMethodGPG:
		return verifyGPG(in.Artifact, resolved.signaturePath, in.PublicKey)
	case domainrelease.SignMethodSigstore:
		return verifyCosign(ctx, cosignVerifier, domainrelease.BlobVerifyRequest{
			Artifact:           in.Artifact,
			BundlePath:         resolved.signaturePath,
			Keyless:            true,
			CertIdentityRegexp: in.CertIdentityRegexp,
			CertOIDCIssuer:     in.CertOIDCIssuer,
		}, out)
	case domainrelease.SignMethodKMS:
		return verifyCosign(ctx, cosignVerifier, domainrelease.BlobVerifyRequest{
			Artifact:   in.Artifact,
			BundlePath: resolved.signaturePath,
			KeyRef:     in.KeyRef,
		}, out)
	default:
		return fmt.Errorf("validate artifact-signature: method %q unsupported: %w", resolved.method, errs.ErrInvalidConfig)
	}
}

// cosignBlobVerifier is the slice of *cosign.Adapter that
// VerifyArtifactSignature needs. Lets tests inject an in-process
// fake without paying for a real subprocess.
type cosignBlobVerifier interface {
	VerifyBlob(ctx context.Context, in domainrelease.BlobVerifyRequest, errOut io.Writer) error
}

// resolvedLayout captures what the on-disk inspection turned up.
type resolvedLayout struct {
	method        domainrelease.SignMethod
	signaturePath string
}

// resolveSignatureLayout inspects the sidecar files next to the
// artifact and returns the method that produced them. Operator
// overrides (explicit Method, SignaturePath) win over the inspection
// result.
//
// Detection rules:
//
//   - <artifact>.bundle present → cosign (sigstore OR kms, per flags)
//   - <artifact>.asc present     → gpg
//   - none of the above          → error
//
// When .bundle AND .asc both exist (dual-sign transitional state),
// the caller must pass --method explicitly — the auto-detect can't
// guess intent there.
//
// For .bundle, the gpg-vs-cosign distinction is unambiguous (cosign).
// The keyless-vs-kms distinction inside cosign is resolved from
// flags: --key present → kms; --cert-identity-regexp present →
// sigstore. Both empty / neither → error at verify time.
func resolveSignatureLayout(in ArtifactSignatureInput) (resolvedLayout, error) {
	hasBundle := regularFileExists(in.Artifact + ".bundle")
	hasAsc := regularFileExists(in.Artifact + ".asc")

	if in.Method == "" && hasBundle && hasAsc {
		return resolvedLayout{}, fmt.Errorf(
			"validate artifact-signature: both %s.bundle and %s.asc present (dual-signed state); pass --method=gpg|sigstore|kms explicitly: %w",
			in.Artifact, in.Artifact, errs.ErrInvalidConfig,
		)
	}

	method := in.Method
	if method == "" {
		auto, err := autoDetectSignMethod(in, hasBundle, hasAsc)
		if err != nil {
			return resolvedLayout{}, err
		}

		method = auto
	}

	sigPath := in.SignaturePath
	if sigPath == "" {
		sigPath = defaultSigPath(in.Artifact, method)
	}

	return resolvedLayout{
		method:        method,
		signaturePath: sigPath,
	}, nil
}

// autoDetectSignMethod picks a SignMethod from the sidecar files
// present next to the artifact, plus the identity flags the caller
// supplied. Called only when --method is not explicit.
//
// .bundle alone is ambiguous between sigstore and kms — pick based on
// which identity constraint the caller supplied. .asc → gpg. No
// sidecar → error.
func autoDetectSignMethod(in ArtifactSignatureInput, hasBundle, hasAsc bool) (domainrelease.SignMethod, error) {
	switch {
	case hasBundle:
		switch {
		case in.KeyRef != "":
			return domainrelease.SignMethodKMS, nil
		case in.CertIdentityRegexp != "":
			return domainrelease.SignMethodSigstore, nil
		default:
			return "", fmt.Errorf(
				"validate artifact-signature: %s.bundle present but no identity constraint supplied — pass --key (kms) or --cert-identity-regexp (sigstore): %w",
				in.Artifact, errs.ErrMissingInput,
			)
		}
	case hasAsc:
		return domainrelease.SignMethodGPG, nil
	default:
		return "", fmt.Errorf(
			"validate artifact-signature: no signature sidecar next to %s (looked for .bundle, .asc): %w",
			in.Artifact, errs.ErrMissingInput,
		)
	}
}

func defaultSigPath(artifact string, method domainrelease.SignMethod) string {
	exts := method.SignatureExtensions()
	if len(exts) == 0 {
		return ""
	}

	return artifact + exts[0]
}

func verifyGPG(artifactPath, signaturePath string, pubKeyArmor []byte) error {
	if len(pubKeyArmor) == 0 {
		return fmt.Errorf("validate artifact-signature (gpg): --public-key is required: %w", errs.ErrMissingInput)
	}

	artifact, err := os.Open(artifactPath) //nolint:gosec // caller-controlled artifact path.
	if err != nil {
		return fmt.Errorf("open artifact %q: %w", artifactPath, err)
	}

	defer func() { _ = artifact.Close() }()

	sig, err := os.Open(signaturePath) //nolint:gosec // caller-controlled signature path.
	if err != nil {
		return fmt.Errorf("open signature %q: %w", signaturePath, err)
	}

	defer func() { _ = sig.Close() }()

	return openpgp.VerifyDetachedArmored(artifact, sig, pubKeyArmor)
}

func verifyCosign(ctx context.Context, verifier cosignBlobVerifier, in domainrelease.BlobVerifyRequest, errOut io.Writer) error {
	return verifier.VerifyBlob(ctx, in, errOut)
}

// regularFileExists reports whether path is a regular file. Used
// for sidecar detection; non-NotExist stat errors (permission
// denied, symlink loop) are treated as "absent" — the actual
// verify step will surface them when it tries to read the file.
func regularFileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return false
		}

		return false
	}

	return info.Mode().IsRegular()
}
