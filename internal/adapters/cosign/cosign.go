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
	"time"

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

// flagTrustedRoot supplies the trust material cosign uses to verify a signature
// it has just made. cosign always performs that self-check, and without this
// flag it fetches the trust root from the public Sigstore TUF CDN — so this is
// what stops a signing run reaching the network at all.
const flagTrustedRoot = "--trusted-root"

// flagInsecureIgnoreTlog drops the transparency-log requirement from a verify
// subcommand. cosign names it "insecure" and prints its own warning; we pass it
// through under that name rather than a friendlier one so the trade-off is not
// disguised at any layer.
const flagInsecureIgnoreTlog = "--insecure-ignore-tlog"

// EnvTransparency names the environment variable that selects whether cosign
// publishes each signature to the public Sigstore transparency log. Its values
// are release.Transparency's: "public" (or unset) and "none".
//
// Why an environment variable rather than a per-command flag: every cosign
// write subcommand (sign-blob, sign, attest) must honour it, the adapter is
// constructed at ~16 call sites, and the failure mode of missing one is silent
// — the signature simply goes to the public Rekor log instead. Resolving it in
// New/NewIsolated means a signing path cannot opt out by forgetting to thread a
// flag, and a NEW signing command inherits the setting for free. That property
// is the point; discoverability is served by sign.transparency in artifacts.yml,
// which is the front door operators actually use.
//
// Why ONE variable and not two: cosign couples the halves. A signature made
// with no transparency log cannot be verified without --insecure-ignore-tlog,
// so a "don't publish" setting and an "don't require a log" setting must always
// agree. Two independent knobs that must agree is a footgun, not a safeguard —
// set one without the other and cosign fails with "not enough verified log
// entries", which tells the operator nothing. One setting drives both halves,
// and the disagreement is unrepresentable.
const EnvTransparency = "REUSABLE_CI_COSIGN_TRANSPARENCY"

// TransparencyFromEnv resolves $REUSABLE_CI_COSIGN_TRANSPARENCY, defaulting to
// release.DefaultTransparency (public) when unset or unparsable.
//
// Unparsable falls back to the SAFE value rather than erroring: this runs in a
// constructor with nowhere to return an error, and the honest failure mode for
// a typo'd value is "we published when you may not have wanted to", not "we
// silently withheld the evidence". The typo is caught properly upstream, where
// sign.transparency is validated against the same enum with a real error.
func TransparencyFromEnv() release.Transparency {
	parsed, err := release.ParseTransparency(strings.TrimSpace(os.Getenv(EnvTransparency)))
	if err != nil {
		return release.DefaultTransparency
	}

	return parsed
}

// emptyTrustedRoot is a Sigstore trusted root naming no services — the
// counterpart to the signing config, and byte-identical to what
// `cosign trusted-root create` emits with no service flags.
//
// It exists because a signing config alone does not make a run offline. cosign
// verifies each signature it has just written, and to do that it needs a trust
// root; absent one it fetches it from the public Sigstore TUF CDN. So a run
// configured to publish nothing would still announce itself to
// tuf-repo-cdn.sigstore.dev on every signature — measured, once per sign, not
// cached away.
//
// Empty is the honest value rather than a stub: the matching signing config
// names no Fulcio, no Rekor and no TSA, so there is no service whose material
// the self-check could meaningfully verify against. The signature itself is
// still checked — against the key, which is where a kms signature's trust
// actually comes from. Passed only when the transparency log is off; the public
// path keeps cosign's own trust root, which it must.
const emptyTrustedRoot = `{"mediaType":"application/vnd.dev.sigstore.trustedroot+json;version=0.1"}`

// Adapter wraps the cosign binary. Bin is overridable for tests.
type Adapter struct {
	Bin string // empty → "cosign"

	// Env, when non-nil, replaces the cosign subprocess environment
	// (exec.Cmd.Env) wholesale. nil → inherit the parent environment
	// (the default). Set via NewIsolated to restrict which secrets the
	// signing subprocess can read.
	Env []string

	// Transparency decides whether each signature is published to the
	// public Sigstore Rekor log. The zero value is empty, which
	// PublishesToLog treats as publishing — so an Adapter built as a bare
	// literal behaves like cosign's own default rather than silently
	// withholding evidence.
	//
	// It drives BOTH halves: write subcommands get --signing-config
	// pointing at a no-transparency-log config, and verify subcommands get
	// --insecure-ignore-tlog. cosign requires them to agree; see
	// EnvTransparency for why that is one setting and not two.
	//
	// Resolved by New/NewIsolated from $REUSABLE_CI_COSIGN_TRANSPARENCY.
	Transparency release.Transparency
}

// New returns an Adapter using the system cosign, with the transparency
// setting resolved from the environment.
func New() *Adapter {
	return &Adapter{Transparency: TransparencyFromEnv()}
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

	args, cleanup, err := a.withSigningConfig([]string{"sign-blob", flagYes, "--bundle", in.BundlePath},
		signingConfigInput{FulcioURL: in.FulcioURL, OIDCIssuer: in.OIDCIssuer, RekorURL: in.RekorURL})
	if err != nil {
		return err
	}

	defer cleanup()

	switch {
	case in.Keyless:
		// The issuer, CA and log are named in the signing config rather than on
		// argv: cosign 3.x deprecated the service flags and refuses them
		// alongside a config. cosign still infers the TOKEN from the runner.

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

	args, cleanup, err := a.withSigningConfig([]string{"sign", flagYes},
		signingConfigInput{FulcioURL: in.FulcioURL, OIDCIssuer: in.OIDCIssuer, RekorURL: in.RekorURL})
	if err != nil {
		return err
	}

	defer cleanup()

	if in.Recursive {
		args = append(args, "--recursive")
	}

	switch {
	case in.Keyless:

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

	args, cleanup, err := a.withSigningConfig([]string{"attest", flagYes, "--type", in.PredicateType, "--predicate", in.PredicatePath},
		signingConfigInput{FulcioURL: in.FulcioURL, OIDCIssuer: in.OIDCIssuer, RekorURL: in.RekorURL})
	if err != nil {
		return err
	}

	defer cleanup()

	if in.Recursive {
		args = append(args, "--recursive")
	}

	switch {
	case in.Keyless:

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

// publishesToLog reports whether this adapter writes transparency-log entries.
// The zero-value Adapter publishes, matching cosign's own default.
func (a *Adapter) publishesToLog() bool { return a.Transparency.PublishesToLog() }

// withSigningConfig appends the flags that keep a signing run off the public
// Sigstore services when the transparency log is turned off, materialising the
// two documents they point at. It is applied by every cosign subcommand that
// writes a signature, so the choice is made in exactly one place. The returned
// cleanup removes the temporary files and is always non-nil, so callers can
// defer it unconditionally.
//
// BOTH flags are needed, and the second is not obvious. --signing-config alone
// stops the Rekor upload but leaves the run reaching tuf-repo-cdn.sigstore.dev
// on every signature, because cosign verifies what it has just signed and
// fetches the trust root to do it. --trusted-root supplies that material
// locally. Together they take a signing run from "publishes nothing" to "makes
// no outbound connection at all", which is the property worth having: it is
// what a reader assumes transparency=none already means, and unlike the first
// it can be enforced by a test rather than reviewed.
//
// Files are written per call rather than cached on the Adapter: a signing run
// is short, the documents are ~100 bytes each, and per-call scope means there
// is no lifetime to manage and nothing to leak into the runner's temp dir.
func (a *Adapter) withSigningConfig(args []string, services signingConfigInput) ([]string, func(), error) {
	noop := func() {}

	services.PublishesTo = a.publishesToLog()
	if !services.needed() {
		return args, noop, nil
	}

	document, err := buildSigningConfig(services, time.Now())
	if err != nil {
		return nil, noop, err
	}

	config, cleanConfig, err := writeTempJSON("signing-config", string(document))
	if err != nil {
		return nil, noop, err
	}

	args = append(args, flagSigningConfig, config)

	// The trust root is only supplied when nothing is published. With a log in
	// play cosign needs the real trust material to verify what it just wrote,
	// and an empty root would deny it that; with no log there is no service
	// whose material it could meaningfully check, and supplying an empty root is
	// what stops it reaching the public TUF CDN to look.
	if services.PublishesTo {
		return args, cleanConfig, nil
	}

	root, cleanRoot, err := writeTempJSON("trusted-root", emptyTrustedRoot)
	if err != nil {
		cleanConfig()

		return nil, noop, err
	}

	cleanup := func() { cleanConfig(); cleanRoot() }

	return append(args, flagTrustedRoot, root), cleanup, nil
}

// writeTempJSON materialises one of the Sigstore documents above into a
// temporary file, returning its path and a cleanup that removes it. The cleanup
// is always non-nil, including on the error paths, so callers can defer it
// without a nil check.
func writeTempJSON(name, content string) (string, func(), error) {
	noop := func() {}

	file, err := os.CreateTemp("", "reusable-ci-"+name+"-*.json")
	if err != nil {
		return "", noop, fmt.Errorf("create %s: %w", name, err)
	}

	cleanup := func() { _ = os.Remove(file.Name()) }

	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()

		cleanup()

		return "", noop, fmt.Errorf("write %s: %w", name, err)
	}

	if err := file.Close(); err != nil {
		cleanup()

		return "", noop, fmt.Errorf("close %s: %w", name, err)
	}

	return file.Name(), cleanup, nil
}

// withTlogPolicy appends --insecure-ignore-tlog when verification has been told
// not to require a log inclusion proof. Applied by every cosign subcommand that
// verifies, so the policy is decided in exactly one place.
func (a *Adapter) withTlogPolicy(args []string) []string {
	if a.publishesToLog() {
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
