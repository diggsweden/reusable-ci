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

// planScopeProvenance is the plan-file scope of `release provenance` in
// $REUSABLE_CI_PLAN (flag > plan > env > default).
const planScopeProvenance = "release provenance"

// flagExternalParametersJSON is the generic externalParameters-extras
// flag: a JSON object merged into buildDefinition.externalParameters
// with every engine-computed key reserved.
const flagExternalParametersJSON = "external-parameters-json"

// provenanceCmd generates an in-toto/SLSA-v1.0 provenance statement from a
// checksums file, emitting the JSON only; signing (cosign sign-blob) is a
// separate step.
//
// Generic provenance uses the same runner-attested source and identity as
// `container attest`, without a forge attestation API. The forgejo-actions
// profile preserves forgejo-ci's shipped provider-derived source/builder shape.
func provenanceCmd() *cli.Command {
	return &cli.Command{
		Name:  "provenance",
		Usage: "generate an in-toto/SLSA-v1.0 provenance statement from a checksums file",
		Description: `Reads GoReleaser-format checksums and emits a signed-ready in-toto
Statement (SLSA Provenance v1.0). Sign the output with cosign sign-blob.

Generic context (repository, ref, commit, builder and run) comes from the
active runner's attested environment, independently of --provider. Unknown
local identity is not inferred; generation requires a CI source and builder.
The forgejo-actions profile preserves provider-derived legacy source/builder
identity. Build timestamps come from flags or $SOURCE_DATE_EPOCH.
Every flag may also be fed from the $REUSABLE_CI_PLAN plan file under the
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

				repoURL, ref, sha := cienv.ProvenanceSource()
				builderID := cienv.ProvenanceBuilderID()

				if profile == apprelease.ProvenanceProfileForgejoActions {
					// Preserve the explicitly selected compatibility profile;
					// target metadata must not feed the generic trust identity.
					evt, contextErr := d.Provider.ResolveContext(ctx)
					if contextErr != nil {
						return contextErr
					}

					repoURL, ref, sha = evt.RepoURL, evt.RefName, evt.SHA
					builderID = forgejoActionsBuilderID(repoURL, ref, cmd.String("workflow"))
				} else if repoURL == "" {
					return fmt.Errorf("provenance: runner repository URL is unknown: %w", errs.ErrMissingInput)
				}

				startedOn, err := resolveStartedOn(ctx, git.New(), cmd.String("started-on"), cmd.String("started-on-commit"))
				if err != nil {
					return err
				}

				checksums, err := cliio.ReadFile(cmd.String(flagChecksumFile))
				if err != nil {
					return err
				}

				out, err := apprelease.GenerateProvenance(apprelease.ProvenanceInput{
					Checksums:     bytes.NewReader(checksums),
					GoSum:         openGoSum(cmd.String("go-sum")),
					RepositoryURL: repoURL,
					Ref:           ref,
					SHA:           sha,
					// Generic provenance uses the same forge-neutral builder /
					// invocation identity as containers. The forgejo-actions profile
					// overrides builderID above to match the provider-specific legacy shape.
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
					endpoints:  signflags.ReadEndpoints(cmd),
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
	// endpoints are the self-hosted Sigstore overrides signflags.Cosign
	// declares (--fulcio-url / --rekor-url / --trusted-root). They were
	// declared here and read nowhere, so a self-hosted operator's provenance
	// was silently signed against the public CA and log.
	endpoints signflags.Endpoints
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

	return cosign.ForKeyRef(in.keyRef).SignBlob(ctx, blob, os.Stderr)
}

// provenanceSignBlobInput maps the method/key decision onto a cosign
// SignBlobInput. A bare --key with no --method is treated as kms
// (cosign --key) for backward compatibility. gpg is rejected — OpenPGP
// signs differently and is not a cosign blob backend. The Sigstore
// endpoints ride along for keyless signing and, as the `release sign`
// signer already enforces, --fulcio-url / --rekor-url are refused for kms.
func provenanceSignBlobInput(in signProvenanceInput, bundle string) (cosign.SignBlobInput, error) {
	blob := cosign.SignBlobInput{Artifact: in.output, BundlePath: bundle}

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
		blob.FulcioURL = in.endpoints.FulcioURL
		blob.RekorURL = in.endpoints.RekorURL
		blob.TrustedRootPath = in.endpoints.TrustedRootPath
	case domainrelease.SignMethodKMS:
		if in.keyRef == "" {
			return blob, fmt.Errorf("provenance: --method=kms requires --key: %w", errs.ErrUsage)
		}

		if in.endpoints.FulcioURL != "" || in.endpoints.RekorURL != "" {
			return blob, fmt.Errorf("provenance: --fulcio-url and --rekor-url are forbidden for --method=kms: %w", errs.ErrUsage)
		}

		blob.KeyRef = in.keyRef
		blob.TrustedRootPath = in.endpoints.TrustedRootPath
	default: // gpg or any non-cosign-blob backend
		return blob, fmt.Errorf("provenance: --method %q cannot sign a blob with cosign (use sigstore or kms): %w", in.method, errs.ErrUsage)
	}

	return blob, nil
}

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
