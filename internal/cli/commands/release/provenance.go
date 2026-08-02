// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/planfile"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/signflags"
	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
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
// planScopeProvenance is the plan-file scope of `release provenance` in
// $REUSABLE_CI_PLAN (flag > plan > env > default).
const planScopeProvenance = "release provenance"

// flagExternalParametersJSON is the generic externalParameters-extras
// flag: a JSON object merged into buildDefinition.externalParameters
// with every engine-computed key reserved.
const flagExternalParametersJSON = "external-parameters-json"

func provenanceCmd() *cli.Command {
	return &cli.Command{
		Name:  "provenance",
		Usage: "generate an in-toto/SLSA-v1.0 provenance statement from a checksums file",
		Description: `Reads GoReleaser-format checksums and emits a signed-ready in-toto
Statement (SLSA Provenance v1.0). Sign the output with cosign sign-blob.

Context (repository, ref, commit) is read from the active provider; the
workflow, run id, and build timestamp come from flags/env. The build
timestamp defaults to $SOURCE_DATE_EPOCH for reproducibility. Every flag
may also be fed from the $REUSABLE_CI_PLAN plan file under the
"release provenance" scope (flag > plan > env > default).

EXAMPLE:
   reusable-ci release provenance --checksum-file checksums.sha256 --method sigstore --output provenance.json`,
		Flags: append(
			[]cli.Flag{
				&cli.StringFlag{Name: flagChecksumFile, Value: cliio.StdSentinel, Sources: planfile.Vars(planScopeProvenance, flagChecksumFile, "CHECKSUM_FILE"), Usage: "GoReleaser checksums file (\"-\" reads stdin)"},
				&cli.StringFlag{Name: "go-sum", Value: "go.sum", Sources: planfile.Vars(planScopeProvenance, "go-sum", "GO_SUM_FILE"), Usage: "go.sum for resolved module deps (empty string to skip)"},
				&cli.StringFlag{Name: "profile", Value: provenanceProfileGenericName, Sources: planfile.Vars(planScopeProvenance, "profile"), Usage: "provenance profile: generic or forgejo-actions"},
				&cli.StringFlag{Name: "workflow", Sources: planfile.Vars(planScopeProvenance, "workflow", "FORGEJO_WORKFLOW"), Usage: "workflow filename/path for the forgejo-actions profile"},
				&cli.StringFlag{Name: "started-on", Sources: planfile.Vars(planScopeProvenance, "started-on"), Usage: "RFC3339 build timestamp (default: $SOURCE_DATE_EPOCH)"},
				&cli.StringFlag{Name: "started-on-commit", Sources: planfile.Vars(planScopeProvenance, "started-on-commit"), Usage: "commit/ref whose commit timestamp becomes the RFC3339 build timestamp"},
				&cli.StringFlag{Name: flagExternalParametersJSON, Sources: planfile.Vars(planScopeProvenance, flagExternalParametersJSON, "SLSA_EXTERNAL_PARAMETERS_JSON"), Usage: "JSON object of extra buildDefinition.externalParameters merged into the predicate; already-present (engine-computed) keys are reserved, a collision fails. Generic successor to the per-field lineage flags (--base-ref/--base-digest/--base-input-id)"},
				&cli.StringFlag{Name: flagOutput, Value: cliio.StdSentinel, Sources: planfile.Vars(planScopeProvenance, flagOutput), Usage: "output file (\"-\" for stdout)"},
				&cli.StringFlag{Name: "bundle", Sources: planfile.Vars(planScopeProvenance, "bundle"), Usage: "signature bundle output path (default: <output>.bundle)"},
			},
			signflags.Cosign(signflags.CosignOpts{
				MethodNote: "Omit to generate the statement only (signs when --method or --key is set).",
				KeyNote:    "Empty = generate only.",
				PlanScope:  planScopeProvenance,
			})...,
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				profile, err := parseProvenanceProfile(cmd.String("profile"))
				if err != nil {
					return err
				}

				// Parse the declared extras up front so a malformed
				// document fails before any context resolution or output.
				extraParams, err := provenance.ParseExternalParametersJSON(cmd.String(flagExternalParametersJSON))
				if err != nil {
					return fmt.Errorf("provenance: --%s: %w", flagExternalParametersJSON, err)
				}

				evt, err := d.Provider.ResolveContext(ctx)
				if err != nil {
					return err
				}

				startedOn, err := resolveStartedOn(ctx, git.New(), cmd.String("started-on"), cmd.String("started-on-commit"))
				if err != nil {
					return err
				}

				checksums, err := cliio.ReadFile(cmd.String(flagChecksumFile))
				if err != nil {
					return err
				}

				builderID := cienv.ProvenanceBuilderID()
				if profile == apprelease.ProvenanceProfileForgejoActions {
					builderID = forgejoActionsBuilderID(evt.RepoURL, evt.RefName, cmd.String("workflow"))
				}

				out, err := apprelease.GenerateProvenance(apprelease.ProvenanceInput{
					Checksums:     bytes.NewReader(checksums),
					GoSum:         openGoSum(cmd.String("go-sum")),
					RepositoryURL: evt.RepoURL,
					Ref:           evt.RefName,
					SHA:           evt.SHA,
					// Generic provenance uses the same forge-neutral builder /
					// invocation identity as containers. The forgejo-actions profile
					// overrides builderID above to match forgejo-ci's shipped shape.
					BuilderID:          builderID,
					InvocationID:       cienv.ProvenanceInvocationID(),
					StartedOn:          startedOn,
					Profile:            profile,
					Workflow:           cmd.String("workflow"),
					ExternalParameters: extraParams,
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

type commitUnixTimer interface {
	CommitUnixTime(ctx context.Context, ref string) (string, error)
}

// resolveStartedOn returns the reproducible build timestamp as RFC3339 UTC.
// Precedence: an explicit --started-on (validated as RFC3339), then a commit
// timestamp requested with --started-on-commit, then $SOURCE_DATE_EPOCH (unix
// seconds, the repo-wide reproducibility convention). One of these is required
// — a provenance without an honest timestamp is refused.
func resolveStartedOn(ctx context.Context, git commitUnixTimer, flagVal, commitRef string) (string, error) {
	if flagVal != "" {
		if _, err := time.Parse(time.RFC3339, flagVal); err != nil {
			return "", fmt.Errorf("provenance: --started-on %q is not RFC3339: %w", flagVal, errs.ErrUsage)
		}

		return flagVal, nil
	}

	if commitRef != "" {
		epoch, err := git.CommitUnixTime(ctx, commitRef)
		if err != nil {
			return "", fmt.Errorf("provenance: resolve --started-on-commit %q: %w", commitRef, err)
		}

		secs, err := strconv.ParseInt(strings.TrimSpace(epoch), 10, 64)
		if err != nil {
			return "", fmt.Errorf("provenance: commit timestamp for %q is not unix seconds: %w", commitRef, errs.ErrUsage)
		}

		return time.Unix(secs, 0).UTC().Format(time.RFC3339), nil
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

// provenanceProfileGenericName is the CLI name of the generic (default)
// provenance profile.
const provenanceProfileGenericName = "generic"

func parseProvenanceProfile(raw string) (apprelease.ProvenanceProfile, error) {
	switch raw {
	case "", provenanceProfileGenericName:
		return apprelease.ProvenanceProfileGeneric, nil
	case string(apprelease.ProvenanceProfileForgejoActions):
		return apprelease.ProvenanceProfileForgejoActions, nil
	default:
		return "", fmt.Errorf("provenance: unknown --profile %q (use generic or forgejo-actions): %w", raw, errs.ErrUsage)
	}
}

func forgejoActionsBuilderID(repoURL, ref, workflow string) string {
	return strings.TrimRight(repoURL, "/") + "/" + forgejoActionsWorkflowPath(workflow) + "@" + ref
}

func forgejoActionsWorkflowPath(workflow string) string {
	if strings.HasPrefix(workflow, ".forgejo/workflows/") {
		return workflow
	}

	return ".forgejo/workflows/" + workflow
}
