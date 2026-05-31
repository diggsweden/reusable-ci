// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package cosign shells out to the system `cosign` binary for
// Sigstore-keyless and KMS-backed artefact signing. The runtime
// image bakes cosign in; local runs need it on PATH.
//
// This adapter only handles the cosign subprocess. The dispatch
// decision ("use cosign vs. use openpgp") lives at the app layer
// (internal/app/release/sign.go).
package cosign

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
)

// flagYes is cosign's non-interactive confirmation flag, shared by every
// write subcommand (sign-blob, sign, attest).
const flagYes = "--yes"

// Adapter wraps the cosign binary. Bin is overridable for tests.
type Adapter struct {
	Bin string // empty → "cosign"

	// Env, when non-nil, replaces the cosign subprocess environment
	// (exec.Cmd.Env) wholesale. nil → inherit the parent environment
	// (the default). Set via NewIsolated to restrict which secrets the
	// signing subprocess can read.
	Env []string
}

// New returns an Adapter using the system cosign.
func New() *Adapter { return &Adapter{} }

// SignBlobInput drives a single cosign sign-blob invocation. The
// adapter never invents file paths: the caller decides where the
// bundle lands. cosign 3.x emits the v3 bundle format exclusively —
// one self-contained JSON sidecar holding signature, optional
// Fulcio certificate, and Rekor proof.
type SignBlobInput struct {
	// Artefact is the file being signed.
	Artefact string

	// BundlePath receives the Sigstore v3 bundle JSON. Required for
	// every method (cosign 3.x removed the split sig+cert layout).
	BundlePath string

	// Keyless selects Sigstore-keyless mode. When true, KeyRef must
	// be empty; the adapter passes --yes (acknowledging the
	// transparency-log upload) and --oidc-issuer when set.
	Keyless bool

	// OIDCIssuer is the OIDC issuer URL for keyless signing. Empty
	// lets cosign auto-detect from the runner. Must be empty when
	// Keyless is false.
	OIDCIssuer string

	// KeyRef is the cosign --key argument: a KMS URI
	// (awskms://, gcpkms://, hashivault://, …), a PKCS#11 URI, or
	// a local key-file path. Must be empty when Keyless is true.
	KeyRef string
}

// SignBlob runs `cosign sign-blob` with the configured input. errOut
// receives a redacted view of cosign's stderr — the redactor strips
// any PEM private-key block or JWT-shaped token before propagating
// the bytes, so a misbehaving cosign release cannot leak key material
// into our logs.
//
// Returns:
//
//   - nil when cosign exits 0 and the signature file is on disk.
//   - errs.ErrUsage when the input is inconsistent (keyless + key,
//     missing artefact, etc.).
//   - cosign's exit-error otherwise; the underlying *exec.ExitError
//     is preserved via %w so callers can extract the exit code.
//
//nolint:cyclop // input validation + argv build + run + classify — phases of one operation.
func (a *Adapter) SignBlob(ctx context.Context, in SignBlobInput, errOut io.Writer) error {
	if err := in.validate(); err != nil {
		return err
	}

	args := []string{"sign-blob", flagYes, "--bundle", in.BundlePath}

	switch {
	case in.Keyless:
		// cosign infers OIDC token from the runner; --oidc-issuer
		// is only set when the caller wants to override (e.g.
		// self-hosted Fulcio, GitLab, Forgejo).
		if in.OIDCIssuer != "" {
			args = append(args, "--oidc-issuer", in.OIDCIssuer)
		}
	default:
		args = append(args, "--key", in.KeyRef)
	}

	args = append(args, in.Artefact)

	return a.run(ctx, errOut, args...)
}

// VerifyBlobInput drives `cosign verify-blob`. Like SignBlobInput,
// the caller supplies every path; the adapter never guesses.
// cosign 3.x verify consumes a single bundle file and requires the
// caller to declare identity constraints (regexp + issuer for
// keyless; pubkey for KMS) — the bundle itself doesn't encode trust.
type VerifyBlobInput struct {
	// Artefact is the file whose signature is being verified.
	Artefact string

	// BundlePath is the v3 bundle JSON sidecar (produced by Sign-
	// Blob). Required for every method.
	BundlePath string

	// Keyless verification — requires CertIdentityRegexp and
	// CertOIDCIssuer. KeyRef must be empty.
	Keyless            bool
	CertIdentityRegexp string
	CertOIDCIssuer     string

	// KeyRef is required for non-keyless verify. Local pubkey path
	// or KMS URI (cosign reads pubkey from KMS).
	KeyRef string
}

// VerifyBlob runs `cosign verify-blob`. Exit 0 → signature verifies;
// non-zero → returns cosign's exit-error wrapped with the argv that
// produced it.
//
// The caller is responsible for picking the right backend
// (Keyless vs. KeyRef) and providing the matching identity
// constraints (certificate-identity-regexp + oidc-issuer for
// keyless, pubkey for KMS).
//
//nolint:cyclop // input validation + argv build + run + classify — phases of one operation.
func (a *Adapter) VerifyBlob(ctx context.Context, in VerifyBlobInput, errOut io.Writer) error {
	if err := in.validate(); err != nil {
		return err
	}

	args := []string{"verify-blob", "--bundle", in.BundlePath, "--new-bundle-format"}

	switch {
	case in.Keyless:
		args = append(args,
			"--certificate-identity-regexp", in.CertIdentityRegexp,
			"--certificate-oidc-issuer", in.CertOIDCIssuer,
		)
	default:
		args = append(args, "--key", in.KeyRef)
	}

	args = append(args, in.Artefact)

	return a.run(ctx, errOut, args...)
}

// validate checks SignBlobInput consistency before any subprocess
// runs. Keeps the error surfaces narrow: callers get a single
// ErrUsage with the specific field at fault rather than a cryptic
// cosign-exit-1 with a stack trace.
func (in SignBlobInput) validate() error {
	if in.Artefact == "" {
		return fmt.Errorf("cosign sign: artefact path is empty: %w", errs.ErrUsage)
	}

	if in.BundlePath == "" {
		return fmt.Errorf("cosign sign: bundle path is empty: %w", errs.ErrUsage)
	}

	if in.Keyless && in.KeyRef != "" {
		return fmt.Errorf("cosign sign: keyless mode forbids --key (got %q): %w", in.KeyRef, errs.ErrUsage)
	}

	if !in.Keyless && in.KeyRef == "" {
		return fmt.Errorf("cosign sign: non-keyless mode requires --key: %w", errs.ErrUsage)
	}

	if !in.Keyless && in.OIDCIssuer != "" {
		return fmt.Errorf("cosign sign: --oidc-issuer only applies to keyless mode: %w", errs.ErrUsage)
	}

	return nil
}

// SignImageInput drives a single `cosign sign <image-ref>` invocation.
// Unlike SignBlob, the signature is stored in the OCI registry next
// to the image (as a sibling tag), not on the local filesystem — so
// no BundlePath is required.
type SignImageInput struct {
	// ImageRef is the OCI reference. Must be a fully-qualified
	// digest reference (registry/image@sha256:...) — cosign refuses
	// to sign a mutable tag.
	ImageRef string

	// Recursive walks a manifest-list, signing each per-arch digest
	// referenced by the list in addition to the list itself.
	// Production releases use multi-arch manifest lists, so this is
	// almost always true.
	Recursive bool

	// Keyless selects Sigstore-keyless mode. When true, KeyRef must
	// be empty.
	Keyless bool

	// OIDCIssuer overrides cosign's default Sigstore OIDC issuer.
	// Only meaningful when Keyless is true; must be empty otherwise.
	OIDCIssuer string

	// KeyRef is the cosign --key URI for non-keyless signing
	// (awskms://, hashivault://, etc.). Must be empty when
	// Keyless is true; required otherwise.
	KeyRef string
}

// SignImage runs `cosign sign` against an OCI image reference. The
// signature is uploaded to the registry next to the image — there is
// no local sidecar file (use SignBlob for that).
//
// Returns errs.ErrUsage when the input is inconsistent; cosign's
// exit error otherwise. errOut receives a redacted view of cosign's
// stderr (same redactor as SignBlob).
func (a *Adapter) SignImage(ctx context.Context, in SignImageInput, errOut io.Writer) error {
	if err := in.validate(); err != nil {
		return err
	}

	args := []string{"sign", flagYes}

	if in.Recursive {
		args = append(args, "--recursive")
	}

	switch {
	case in.Keyless:
		if in.OIDCIssuer != "" {
			args = append(args, "--oidc-issuer", in.OIDCIssuer)
		}
	default:
		args = append(args, "--key", in.KeyRef)
	}

	args = append(args, in.ImageRef)

	return a.run(ctx, errOut, args...)
}

// AttestImageInput drives a single `cosign attest <image-ref>`
// invocation. The in-toto attestation is signed and stored in the OCI
// registry next to the image (referrers / `.att`), so it is verifiable
// with `cosign verify-attestation` on any registry/forge — unlike a
// BuildKit in-index attestation, which is unsigned and has no portable
// verifier.
type AttestImageInput struct {
	// ImageRef is a fully-qualified digest reference. cosign refuses a
	// mutable tag — an attestation must bind to immutable content.
	ImageRef string

	// PredicateType is cosign's --type: a well-known name
	// (slsaprovenance, cyclonedx, spdx, …) or a predicate-type URI.
	PredicateType string

	// PredicatePath is the file holding the predicate JSON (the SLSA
	// provenance predicate, the CycloneDX SBOM, …).
	PredicatePath string

	// Recursive attests each per-arch child of a manifest list in
	// addition to the list itself — used for one provenance predicate
	// that applies to the whole multi-arch release.
	Recursive bool

	// Keyless selects Sigstore-keyless mode. KeyRef must be empty.
	Keyless bool

	// OIDCIssuer overrides the default issuer; keyless-only.
	OIDCIssuer string

	// KeyRef is the cosign --key URI for non-keyless attestation.
	KeyRef string
}

// AttestImage runs `cosign attest`, producing a signed in-toto
// attestation stored in the registry alongside the image. Returns
// errs.ErrUsage on inconsistent input; cosign's exit error otherwise.
// errOut receives the redacted cosign stderr (same redactor as Sign).
func (a *Adapter) AttestImage(ctx context.Context, in AttestImageInput, errOut io.Writer) error {
	if err := in.validate(); err != nil {
		return err
	}

	args := []string{"attest", flagYes, "--type", in.PredicateType, "--predicate", in.PredicatePath}

	if in.Recursive {
		args = append(args, "--recursive")
	}

	switch {
	case in.Keyless:
		if in.OIDCIssuer != "" {
			args = append(args, "--oidc-issuer", in.OIDCIssuer)
		}
	default:
		args = append(args, "--key", in.KeyRef)
	}

	args = append(args, in.ImageRef)

	return a.run(ctx, errOut, args...)
}

// validate checks AttestImageInput consistency before any subprocess
// runs, mirroring SignBlobInput.validate — a single ErrUsage naming the
// field at fault instead of a cryptic cosign exit.
func (in AttestImageInput) validate() error {
	if in.ImageRef == "" {
		return fmt.Errorf("cosign attest: image reference is empty: %w", errs.ErrUsage)
	}

	if in.PredicateType == "" {
		return fmt.Errorf("cosign attest: predicate type is empty: %w", errs.ErrUsage)
	}

	if in.PredicatePath == "" {
		return fmt.Errorf("cosign attest: predicate path is empty: %w", errs.ErrUsage)
	}

	if in.Keyless && in.KeyRef != "" {
		return fmt.Errorf("cosign attest: keyless mode forbids --key (got %q): %w", in.KeyRef, errs.ErrUsage)
	}

	if !in.Keyless && in.KeyRef == "" {
		return fmt.Errorf("cosign attest: non-keyless mode requires --key: %w", errs.ErrUsage)
	}

	if !in.Keyless && in.OIDCIssuer != "" {
		return fmt.Errorf("cosign attest: --oidc-issuer only applies to keyless mode: %w", errs.ErrUsage)
	}

	return nil
}

// VerifyImageInput drives `cosign verify <image-ref>`. The signature
// is read from the registry — no local sidecar path is needed.
type VerifyImageInput struct {
	ImageRef string

	// Keyless verification: requires CertIdentityRegexp +
	// CertOIDCIssuer. KeyRef must be empty.
	Keyless            bool
	CertIdentityRegexp string
	CertOIDCIssuer     string

	// KMS verification: requires KeyRef. The other identity fields
	// must be empty.
	KeyRef string
}

// VerifyImage runs `cosign verify` against an OCI image reference.
// Exit 0 → signature verifies and identity constraints match.
func (a *Adapter) VerifyImage(ctx context.Context, in VerifyImageInput, errOut io.Writer) error {
	if err := in.validate(); err != nil {
		return err
	}

	args := []string{"verify"}

	switch {
	case in.Keyless:
		args = append(args,
			"--certificate-identity-regexp", in.CertIdentityRegexp,
			"--certificate-oidc-issuer", in.CertOIDCIssuer,
		)
	default:
		args = append(args, "--key", in.KeyRef)
	}

	args = append(args, in.ImageRef)

	return a.run(ctx, errOut, args...)
}

// CopyImageInput drives `cosign copy`, which copies an image together
// with its registry-attached signatures and attestations from Source to
// Dest, preserving the digest. This is the cross-registry promotion
// primitive: a same-repo moving tag shares the digest-addressed signature,
// but a different repo/registry does not, so the signature must travel
// with the image.
type CopyImageInput struct {
	// Source is the image to copy from (a tag or digest ref).
	Source string

	// Dest is the destination ref (registry/path:tag). The destination
	// registry/repo differs from Source for a genuine cross-registry copy.
	Dest string
}

// CopyImage runs `cosign copy --force <source> <dest>`. --force makes the
// promotion idempotent: re-promoting the same digest (the build-once
// invariant) overwrites the destination tag rather than failing. Both the
// source and destination registries must be authenticated in the
// environment (e.g. a docker login / token for each).
//
// No --only is passed ON PURPOSE: `cosign copy` then carries the FULL evidence
// set — the image, its signatures, attestations (incl. SLSA provenance), and
// SBOMs — so the cross-registry/sovereign destination receives complete
// evidence, not just the image. Do not add --only without re-checking that the
// promoted image still verifies downstream.
//
// Returns errs.ErrUsage on inconsistent input; cosign's exit error
// otherwise. errOut receives a redacted view of cosign's stderr.
func (a *Adapter) CopyImage(ctx context.Context, in CopyImageInput, errOut io.Writer) error {
	if err := in.validate(); err != nil {
		return err
	}

	return a.run(ctx, errOut, "copy", "--force", in.Source, in.Dest)
}

// run invokes cosign with args. cosign's stderr is captured into a
// buffer, run through safeexec.RedactKeyMaterial, and forwarded to
// errOut. This keeps a hypothetical regression in cosign that echoes
// key material on error from leaking into CI logs.
func (a *Adapter) run(ctx context.Context, errOut io.Writer, args ...string) error {
	cmd := safeexec.Command(ctx, a.bin(), args...)
	if a.Env != nil {
		cmd.Env = a.Env
	}

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	err := cmd.Run()

	if errOut != nil {
		_, _ = errOut.Write(safeexec.RedactKeyMaterial(stderr.Bytes()))
	}

	if err != nil {
		return fmt.Errorf("cosign %s: %w", strings.Join(args, " "), err)
	}

	return nil
}

func (a *Adapter) bin() string {
	if a.Bin == "" {
		return "cosign"
	}

	return a.Bin
}

// validate enforces SignImageInput consistency. The image ref must
// be a digest reference — cosign refuses tag-based signing, but we
// surface the requirement early so the operator gets an actionable
// error instead of a cryptic cosign-internal failure.
func (in SignImageInput) validate() error {
	if in.ImageRef == "" {
		return fmt.Errorf("cosign sign image: image reference is empty: %w", errs.ErrUsage)
	}

	if !strings.Contains(in.ImageRef, "@sha256:") {
		return fmt.Errorf(
			"cosign sign image: image reference %q must be a digest reference (registry/image@sha256:...); cosign refuses to sign mutable tags: %w",
			in.ImageRef, errs.ErrUsage,
		)
	}

	if in.Keyless && in.KeyRef != "" {
		return fmt.Errorf("cosign sign image: keyless mode forbids --key (got %q): %w", in.KeyRef, errs.ErrUsage)
	}

	if !in.Keyless && in.KeyRef == "" {
		return fmt.Errorf("cosign sign image: non-keyless mode requires --key: %w", errs.ErrUsage)
	}

	if !in.Keyless && in.OIDCIssuer != "" {
		return fmt.Errorf("cosign sign image: --oidc-issuer only applies to keyless mode: %w", errs.ErrUsage)
	}

	return nil
}

// validate enforces CopyImageInput consistency before the subprocess runs.
func (in CopyImageInput) validate() error {
	if in.Source == "" {
		return fmt.Errorf("cosign copy: source image reference is empty: %w", errs.ErrUsage)
	}

	if in.Dest == "" {
		return fmt.Errorf("cosign copy: destination image reference is empty: %w", errs.ErrUsage)
	}

	return nil
}

// validate mirrors SignImageInput.validate for the verify side.
func (in VerifyImageInput) validate() error {
	if in.ImageRef == "" {
		return fmt.Errorf("cosign verify image: image reference is empty: %w", errs.ErrUsage)
	}

	if !strings.Contains(in.ImageRef, "@sha256:") {
		return fmt.Errorf(
			"cosign verify image: image reference %q must be a digest reference (registry/image@sha256:...); verifying a mutable tag is unsafe: %w",
			in.ImageRef, errs.ErrUsage,
		)
	}

	if in.Keyless {
		if in.CertIdentityRegexp == "" {
			return fmt.Errorf("cosign verify image (keyless): cert-identity-regexp is empty: %w", errs.ErrUsage)
		}

		if in.CertOIDCIssuer == "" {
			return fmt.Errorf("cosign verify image (keyless): cert-oidc-issuer is empty: %w", errs.ErrUsage)
		}

		if in.KeyRef != "" {
			return fmt.Errorf("cosign verify image: keyless mode forbids --key (got %q): %w", in.KeyRef, errs.ErrUsage)
		}

		return nil
	}

	if in.KeyRef == "" {
		return fmt.Errorf("cosign verify image: non-keyless mode requires --key: %w", errs.ErrUsage)
	}

	return nil
}

// validate mirrors SignBlobInput.validate for the verify-blob side.
func (in VerifyBlobInput) validate() error {
	if in.Artefact == "" {
		return fmt.Errorf("cosign verify: artefact path is empty: %w", errs.ErrUsage)
	}

	if in.BundlePath == "" {
		return fmt.Errorf("cosign verify: bundle path is empty: %w", errs.ErrUsage)
	}

	if in.Keyless {
		if in.CertIdentityRegexp == "" {
			return fmt.Errorf("cosign verify (keyless): cert-identity-regexp is empty: %w", errs.ErrUsage)
		}

		if in.CertOIDCIssuer == "" {
			return fmt.Errorf("cosign verify (keyless): cert-oidc-issuer is empty: %w", errs.ErrUsage)
		}

		if in.KeyRef != "" {
			return fmt.Errorf("cosign verify: keyless mode forbids --key (got %q): %w", in.KeyRef, errs.ErrUsage)
		}

		return nil
	}

	if in.KeyRef == "" {
		return fmt.Errorf("cosign verify: non-keyless mode requires --key: %w", errs.ErrUsage)
	}

	return nil
}
