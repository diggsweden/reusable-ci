// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/signflags"
	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

// provenanceCmd generates an in-toto/SLSA-v1.0 provenance statement from
// a checksums file, emitting the JSON only; signing (cosign sign-blob) is
// a separate step.
//
// The predicate is FORGE-NEUTRAL — the same shape the container path emits
// via `container attest` — so it works on any forge whose provider can
// resolve the event context (repository, ref, commit). No per-forge
// provenance profile.
func provenanceCmd() *cli.Command {
	return &cli.Command{
		Name:  "provenance",
		Usage: "generate an in-toto/SLSA-v1.0 provenance statement from a checksums file",
		Description: `Reads GoReleaser-format checksums and emits a signed-ready in-toto
Statement (SLSA Provenance v1.0). Sign the output with cosign sign-blob.

Context (repository, ref, commit) is read from the active provider; the
workflow, run id, and build timestamp come from flags/env. The build
timestamp defaults to $SOURCE_DATE_EPOCH for reproducibility.

EXAMPLE:
   reusable-ci release provenance --checksum-file checksums.sha256 --method sigstore --output provenance.json`,
		Flags: append(
			[]cli.Flag{
				&cli.StringFlag{Name: "checksum-file", Value: cliio.StdSentinel, Sources: cli.EnvVars("CHECKSUM_FILE"), Usage: "GoReleaser checksums file (\"-\" reads stdin)"},
				&cli.StringFlag{Name: "go-sum", Value: "go.sum", Usage: "go.sum for resolved module deps (empty string to skip)"},
				&cli.StringFlag{Name: "started-on", Usage: "RFC3339 build timestamp (default: $SOURCE_DATE_EPOCH)"},
				&cli.StringFlag{Name: flagOutput, Value: cliio.StdSentinel, Usage: "output file (\"-\" for stdout)"},
				&cli.StringFlag{Name: "bundle", Usage: "signature bundle output path (default: <output>.bundle)"},
			},
			signflags.Cosign(signflags.CosignOpts{
				MethodNote: "Omit to generate the statement only (signs when --method or --key is set).",
				KeyNote:    "Empty = generate only.",
			})...,
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				evt, err := d.Provider.ResolveContext(ctx)
				if err != nil {
					return err
				}

				startedOn, err := resolveStartedOn(cmd.String("started-on"))
				if err != nil {
					return err
				}

				checksums, err := cliio.ReadFile(cmd.String("checksum-file"))
				if err != nil {
					return err
				}

				out, err := apprelease.GenerateProvenance(apprelease.ProvenanceInput{
					Checksums:     bytes.NewReader(checksums),
					GoSum:         openGoSum(cmd.String("go-sum")),
					RepositoryURL: evt.RepoURL,
					Ref:           evt.RefName,
					SHA:           evt.SHA,
					// Same forge-neutral builder/invocation identities the
					// container path uses — the canonical workflow ref, not the
					// human-facing workflow display name.
					BuilderID:    cienv.ProvenanceBuilderID(),
					InvocationID: cienv.ProvenanceInvocationID(),
					StartedOn:    startedOn,
				})
				if err != nil {
					return err
				}

				output := cmd.String(flagOutput)
				if err := cliio.WriteFile(output, out, 0o644); err != nil { //nolint:gosec // provenance JSON is public attestation material, not a secret.
					return err
				}

				return signProvenance(ctx, signProvenanceInput{
					output:     output,
					method:     cmd.String("method"),
					keyRef:     cmd.String("key"),
					oidcIssuer: cmd.String("oidc-issuer"),
					bundle:     cmd.String("bundle"),
				})
			})
		},
	}
}

// signProvenanceInput carries the signing decision for the generated
// provenance statement.
type signProvenanceInput struct {
	output     string
	method     string // "" | sigstore | kms
	keyRef     string
	oidcIssuer string
	bundle     string
}

// signProvenance signs the written provenance statement with cosign
// sign-blob, emitting a portable Sigstore bundle (verifiable on any forge
// with `cosign verify-blob`). It is the forge-neutral replacement for
// GitHub's attest-build-provenance:
//
//   - --method=sigstore → keyless OIDC (the workflow's identity).
//   - --method=kms      → cosign --key (KMS URI / file / env://VAR).
//   - neither method nor key → generate-only (no signature).
//
// For an env://VAR key the cosign subprocess is isolated to that key var
// (+ COSIGN_PASSWORD) so no forge token, GPG key, or registry credential
// is visible to it — the forgejo-ci secret-family-isolation guarantee.
// Keyless and KMS/file keys are not isolated (those backends need their
// own OIDC / cloud env).
func signProvenance(ctx context.Context, in signProvenanceInput) error {
	if in.method == "" && in.keyRef == "" {
		return nil // generate-only
	}

	if in.output == cliio.StdSentinel {
		return fmt.Errorf("provenance: --output must be a file when signing (cosign signs a path): %w", errs.ErrUsage)
	}

	bundle := in.bundle
	if bundle == "" {
		bundle = in.output + ".bundle"
	}

	blob, err := provenanceSignBlobInput(in, bundle)
	if err != nil {
		return err
	}

	adapter := cosign.New()
	if allow := provenanceSignAllow(in.keyRef); allow != nil {
		adapter = cosign.NewIsolated(allow...)
	}

	return adapter.SignBlob(ctx, blob, os.Stderr)
}

// provenanceSignBlobInput maps the method/key decision onto a cosign
// SignBlobInput. A bare --key with no --method is treated as kms
// (cosign --key) for backward compatibility. gpg is rejected — OpenPGP
// signs differently and is not a cosign blob backend.
func provenanceSignBlobInput(in signProvenanceInput, bundle string) (cosign.SignBlobInput, error) {
	blob := cosign.SignBlobInput{Artefact: in.output, BundlePath: bundle}

	// A bare --key with no --method means KMS/key signing.
	raw := in.method
	if raw == "" && in.keyRef != "" {
		raw = string(domainrelease.SignMethodKMS)
	}

	method, err := domainrelease.ParseSignMethod(raw)
	if err != nil {
		return blob, fmt.Errorf("provenance: --method %q is not supported (use sigstore or kms): %w", in.method, errs.ErrUsage)
	}

	switch method {
	case domainrelease.SignMethodSigstore:
		blob.Keyless = true
		blob.OIDCIssuer = in.oidcIssuer
	case domainrelease.SignMethodKMS:
		if in.keyRef == "" {
			return blob, fmt.Errorf("provenance: --method=kms requires --key: %w", errs.ErrUsage)
		}

		blob.KeyRef = in.keyRef
	default: // gpg or any non-cosign-blob backend
		return blob, fmt.Errorf("provenance: --method %q cannot sign a blob with cosign (use sigstore or kms): %w", in.method, errs.ErrUsage)
	}

	return blob, nil
}

// provenanceSignAllow returns the secret env vars cosign may read when
// signing with keyRef, or nil when no isolation applies. For an
// env://VAR key, only that var and COSIGN_PASSWORD are allowed; KMS,
// file, and keyless refs return nil (they need their own credential env,
// so isolating them would break signing).
func provenanceSignAllow(keyRef string) []string {
	v, ok := strings.CutPrefix(keyRef, "env://")
	if !ok || v == "" {
		return nil
	}

	return []string{v, cosignPasswordEnv}
}

// cosignPasswordEnv is the env var cosign reads for the signing-key
// passphrase; always allowed alongside an env:// signing key.
const cosignPasswordEnv = "COSIGN_PASSWORD"

// resolveStartedOn returns the reproducible build timestamp as RFC3339
// UTC. Precedence: an explicit --started-on (validated as RFC3339), then
// $SOURCE_DATE_EPOCH (unix seconds, the repo-wide reproducibility
// convention). One of the two is required — a provenance without an
// honest timestamp is refused.
func resolveStartedOn(flagVal string) (string, error) {
	if flagVal != "" {
		if _, err := time.Parse(time.RFC3339, flagVal); err != nil {
			return "", fmt.Errorf("provenance: --started-on %q is not RFC3339: %w", flagVal, errs.ErrUsage)
		}

		return flagVal, nil
	}

	if ts, ok := cienv.SourceDateEpochRFC3339(); ok {
		return ts, nil
	}

	return "", fmt.Errorf("provenance: no build timestamp — pass --started-on <RFC3339> or set a valid $SOURCE_DATE_EPOCH: %w", errs.ErrUsage)
}

// openGoSum returns a reader over the go.sum at path, or nil when the
// path is empty or the file is absent (module deps are best-effort —
// their absence is not fatal).
func openGoSum(path string) io.Reader {
	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path) //nolint:gosec // go.sum path is an operator-supplied build input, not attacker-controlled.
	if err != nil {
		return nil
	}

	return bytes.NewReader(data)
}
