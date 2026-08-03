// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package cosign shells out to the system `cosign` binary for
// Sigstore-keyless and KMS-backed artifact signing. The runtime
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
	"os"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/release"
	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
)

// flagYes is cosign's non-interactive confirmation flag, shared by every
// write subcommand (sign-blob, sign, attest).
const flagYes = "--yes"

// flagSigningConfig selects which Sigstore services a write subcommand uses.
const flagSigningConfig = "--signing-config"

// flagInsecureIgnoreTlog drops the transparency-log requirement from a verify
// subcommand. cosign names it "insecure" and prints its own warning; we pass it
// through under that name rather than a friendlier one so the trade-off is not
// disguised at any layer.
const flagInsecureIgnoreTlog = "--insecure-ignore-tlog"

// EnvSigningConfig names the environment variable that points at a Sigstore
// signing-config file.
//
// Why an environment variable rather than a per-command flag: every cosign
// write subcommand (sign-blob, sign, attest) must honour it, the adapter is
// constructed at ~16 call sites, and the failure mode of missing one is silent
// — the signature simply goes to the public Rekor log instead. Resolving it in
// New/NewIsolated means a signing path cannot opt out by forgetting to thread a
// flag, and a NEW signing command inherits the setting for free. That property
// is the point; discoverability is served by documenting it on the signing
// commands.
const EnvSigningConfig = "REUSABLE_CI_COSIGN_SIGNING_CONFIG"

// SigningConfigFromEnv returns the signing-config path from
// $REUSABLE_CI_COSIGN_SIGNING_CONFIG, or "" when unset.
//
// Empty keeps cosign's built-in public-Sigstore config: keyless signing needs
// Fulcio and a Rekor inclusion proof, so uploading is correct there and is the
// whole point of the transparency log. Set it to a config with no transparency
// log service to sign without publishing — an air-gapped or private-Sigstore
// deployment, or a test suite that must not write to a permanent public log.
// Build one with:
//
//	cosign signing-config create --no-default-rekor --no-default-fulcio \
//	    --no-default-oidc --no-default-tsa --out nolog.json
//
// cosign's own --tlog-upload=false is deprecated in cosign 3.x and errors out
// in favour of this file, so the config is the supported route.
func SigningConfigFromEnv() string { return os.Getenv(EnvSigningConfig) }

// EnvInsecureIgnoreTlog names the environment variable that drops the
// transparency-log requirement from verification. Resolved alongside
// EnvSigningConfig, for the same reason: the verify subcommands are reached
// from many call sites and a missed one fails closed in a confusing way rather
// than obviously.
const EnvInsecureIgnoreTlog = "REUSABLE_CI_COSIGN_INSECURE_IGNORE_TLOG"

// InsecureIgnoreTlogFromEnv reports whether $REUSABLE_CI_COSIGN_INSECURE_IGNORE_TLOG
// is set to a truthy value.
//
// It exists because cosign couples the two halves: verification demands a log
// inclusion proof, so a signature made against a signing config with no
// transparency log CANNOT be verified without this. A trusted root with no log
// service does not relax the requirement — cosign still requires one entry.
//
// The honest reading, in cosign's own words: "Artifacts cannot be publicly
// verified when not included in a log." Turning this on buys the ability to
// verify unlogged signatures and gives up transparency and auditability for
// them. It is the right setting for a test suite that must not write to a
// permanent public log, and for a deployment that has deliberately chosen not
// to run a transparency log. It is the wrong setting for anything whose
// signatures are meant to be publicly verifiable — for that, run a private
// Rekor and point EnvSigningConfig at it instead, which keeps verification
// fully checked.
func InsecureIgnoreTlogFromEnv() bool { return isTruthy(os.Getenv(EnvInsecureIgnoreTlog)) }

// isTruthy accepts the same spellings the rest of the CLI treats as "on".
func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}

	return false
}

// Adapter wraps the cosign binary. Bin is overridable for tests.
type Adapter struct {
	Bin string // empty → "cosign"

	// Env, when non-nil, replaces the cosign subprocess environment
	// (exec.Cmd.Env) wholesale. nil → inherit the parent environment
	// (the default). Set via NewIsolated to restrict which secrets the
	// signing subprocess can read.
	Env []string

	// SigningConfig is a path to a Sigstore signing-config file, passed to
	// every write subcommand as --signing-config. Empty (the default)
	// leaves cosign on its built-in public-Sigstore config, which uploads
	// each signature to the public Rekor transparency log.
	//
	// Resolved by New/NewIsolated from $REUSABLE_CI_COSIGN_SIGNING_CONFIG.
	// See SigningConfigFromEnv for why it is resolved there rather than
	// threaded per command.
	SigningConfig string

	// InsecureIgnoreTlog drops the transparency-log requirement from every
	// verify subcommand. False (the default) keeps verification fully
	// checked. Resolved by New/NewIsolated from
	// $REUSABLE_CI_COSIGN_INSECURE_IGNORE_TLOG; see
	// InsecureIgnoreTlogFromEnv for what it costs.
	InsecureIgnoreTlog bool
}

// New returns an Adapter using the system cosign, with the signing config
// resolved from the environment.
func New() *Adapter {
	return &Adapter{
		SigningConfig:      SigningConfigFromEnv(),
		InsecureIgnoreTlog: InsecureIgnoreTlogFromEnv(),
	}
}

// SignBlobInput drives a single cosign sign-blob invocation. It is
// the domain port request (release.BlobSignRequest) — the alias keeps
// this adapter's call sites reading naturally while the type itself
// lives with the domain, so app-layer consumers construct it without
// importing this adapter.
type SignBlobInput = release.BlobSignRequest

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
//     missing artifact, etc.).
//   - cosign's exit-error otherwise; the underlying *exec.ExitError
//     is preserved via %w so callers can extract the exit code.
//
//nolint:cyclop // input validation + argv build + run + classify — phases of one operation.
func (a *Adapter) SignBlob(ctx context.Context, in SignBlobInput, errOut io.Writer) error {
	if err := in.Validate(); err != nil {
		return err
	}

	args := a.withSigningConfig([]string{"sign-blob", flagYes, "--bundle", in.BundlePath})

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

	args = append(args, in.Artifact)

	return a.run(ctx, errOut, args...)
}

// VerifyBlobInput drives `cosign verify-blob`. It is the domain port
// request (release.BlobVerifyRequest) — see SignBlobInput for why the
// type lives with the domain.
type VerifyBlobInput = release.BlobVerifyRequest

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
	if err := in.Validate(); err != nil {
		return err
	}

	args := a.withTlogPolicy([]string{"verify-blob", "--bundle", in.BundlePath, "--new-bundle-format"})

	switch {
	case in.Keyless:
		args = append(args,
			"--certificate-identity-regexp", in.CertIdentityRegexp,
			"--certificate-oidc-issuer", in.CertOIDCIssuer,
		)
	default:
		args = append(args, "--key", in.KeyRef)
	}

	args = append(args, in.Artifact)

	return a.run(ctx, errOut, args...)
}

// SignImageInput drives a single `cosign sign <image-ref>` invocation.
// It is the domain port request (container.ImageSignRequest) — the
// alias keeps this adapter's call sites reading naturally while the
// type itself lives with the domain, so app-layer consumers construct
// it without importing this adapter.
type SignImageInput = container.ImageSignRequest

// SignImage runs `cosign sign` against an OCI image reference. The
// signature is uploaded to the registry next to the image — there is
// no local sidecar file (use SignBlob for that).
//
// Returns errs.ErrUsage when the input is inconsistent; cosign's
// exit error otherwise. errOut receives a redacted view of cosign's
// stderr (same redactor as SignBlob).
func (a *Adapter) SignImage(ctx context.Context, in SignImageInput, errOut io.Writer) error {
	if err := in.Validate(); err != nil {
		return err
	}

	args := a.withSigningConfig([]string{"sign", flagYes})

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
// AttestImageInput is the domain port request
// (container.ImageAttestRequest) — see SignImageInput for why the type
// lives with the domain.
type AttestImageInput = container.ImageAttestRequest

// PublicKey writes `cosign public-key --key <ref>` to out. It is used by
// signer-boundary commands that need to verify registry-published evidence with
// the public key derived from the private signing material, without exporting
// that public key through workflow shell.
func (a *Adapter) PublicKey(ctx context.Context, keyRef string, out, errOut io.Writer) error {
	if keyRef == "" {
		return fmt.Errorf("cosign public-key: key reference is empty: %w", errs.ErrUsage)
	}

	return a.runWithStdout(ctx, out, errOut, "public-key", "--key", keyRef)
}

// AttestImage runs `cosign attest`, producing a signed in-toto
// attestation stored in the registry alongside the image. Returns
// errs.ErrUsage on inconsistent input; cosign's exit error otherwise.
// errOut receives the redacted cosign stderr (same redactor as Sign).
func (a *Adapter) AttestImage(ctx context.Context, in AttestImageInput, errOut io.Writer) error {
	if err := in.Validate(); err != nil {
		return err
	}

	args := a.withSigningConfig([]string{"attest", flagYes, "--type", in.PredicateType, "--predicate", in.PredicatePath})

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

// VerifyImageInput drives `cosign verify <image-ref>`. It is the
// domain port request (container.ImageVerifyRequest) — see
// SignImageInput for why the type lives with the domain.
type VerifyImageInput = container.ImageVerifyRequest

// VerifyImage runs `cosign verify` against an OCI image reference.
// Exit 0 → signature verifies and identity constraints match.
func (a *Adapter) VerifyImage(ctx context.Context, in VerifyImageInput, errOut io.Writer) error {
	if err := in.Validate(); err != nil {
		return err
	}

	args := a.withTlogPolicy([]string{"verify"})

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

// VerifyAttestationInput drives `cosign verify-attestation <image-ref>`.
// It is the domain port request (container.AttestationVerifyRequest) —
// see SignImageInput for why the type lives with the domain.
type VerifyAttestationInput = container.AttestationVerifyRequest

// VerifyAttestation runs `cosign verify-attestation --type <type>` against an
// OCI image reference. Exit 0 → an attestation of that type verifies and the
// identity constraints match. errOut receives the redacted cosign stderr.
func (a *Adapter) VerifyAttestation(ctx context.Context, in VerifyAttestationInput, errOut io.Writer) error {
	return a.VerifyAttestationOutput(ctx, in, nil, errOut)
}

// VerifyAttestationOutput runs `cosign verify-attestation --type <type>` and
// writes cosign's verified in-toto statement(s) to out. This is used by
// higher-level verifiers that need to inspect predicate fields after cosign has
// checked the signature, identity, and subject binding.
func (a *Adapter) VerifyAttestationOutput(ctx context.Context, in VerifyAttestationInput, out, errOut io.Writer) error {
	if err := in.Validate(); err != nil {
		return err
	}

	args := a.withTlogPolicy([]string{"verify-attestation", "--type", in.PredicateType})

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

	return a.runWithStdout(ctx, out, errOut, args...)
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

// withSigningConfig appends --signing-config when one is configured. It is
// applied by every cosign subcommand that writes a signature, so the choice of
// Sigstore services is made in exactly one place.
func (a *Adapter) withSigningConfig(args []string) []string {
	if a.SigningConfig == "" {
		return args
	}

	return append(args, flagSigningConfig, a.SigningConfig)
}

// withTlogPolicy appends --insecure-ignore-tlog when verification has been told
// not to require a log inclusion proof. Applied by every cosign subcommand that
// verifies, so the policy is decided in exactly one place.
func (a *Adapter) withTlogPolicy(args []string) []string {
	if !a.InsecureIgnoreTlog {
		return args
	}

	return append(args, flagInsecureIgnoreTlog)
}

// run invokes cosign with args. cosign's stderr is captured into a
// buffer, run through safeexec.RedactKeyMaterial, and forwarded to
// errOut. This keeps a hypothetical regression in cosign that echoes
// key material on error from leaking into CI logs.
func (a *Adapter) run(ctx context.Context, errOut io.Writer, args ...string) error {
	return a.runWithStdout(ctx, nil, errOut, args...)
}

func (a *Adapter) runWithStdout(ctx context.Context, out, errOut io.Writer, args ...string) error {
	cmd := safeexec.Command(ctx, a.bin(), args...)
	if a.Env != nil {
		cmd.Env = a.Env
	}

	var stderr bytes.Buffer

	cmd.Stdout = out
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
