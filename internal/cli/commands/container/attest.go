// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/signflags"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

// attestCmd wires `reusable-ci container attest <image>@<digest>`.
//
// It attaches a signed in-toto attestation (SLSA provenance or an SBOM)
// to the image via cosign — registry-attached and verifiable with
// `cosign verify-attestation` on any forge. This is the portable
// replacement for GitHub's actions/attest-* and for relying on
// BuildKit's unsigned in-index attestations.
//
// For --type slsaprovenance1 with no --predicate, the SLSA v1.0 predicate
// is generated from the CI environment (GitHub/Forgejo `GITHUB_*`,
// GitLab `CI_*`), so the workflow call stays a one-liner. Only SLSA v1.0 is
// supported; cosign's obsolete "slsaprovenance" (v0.2) alias is rejected.
func attestCmd() *cli.Command {
	return &cli.Command{
		Name:      "attest",
		Usage:     "attach a signed in-toto attestation (slsaprovenance1 | cyclonedx | spdx) to an OCI image with cosign; registry-attached, verifiable with cosign verify-attestation",
		ArgsUsage: "<registry/image@sha256:...>",
		Description: `EXAMPLES:
   # SLSA v1.0 provenance (predicate generated from the CI environment), keyless
   reusable-ci container attest ghcr.io/org/app@sha256:abc... --type slsaprovenance1 --method sigstore

   # Attach a CycloneDX SBOM as a signed attestation
   reusable-ci container attest ghcr.io/org/app@sha256:abc... --type cyclonedx --predicate sbom.cdx.json --method sigstore`,
		Flags: append(
			append(
				[]cli.Flag{
					&cli.StringFlag{Name: "type", Sources: cli.EnvVars("PREDICATE_TYPE"), Usage: "predicate type: slsaprovenance1 (SLSA v1.0; the obsolete v0.2 'slsaprovenance' is rejected) | cyclonedx | spdx | <uri>"},
					&cli.StringFlag{Name: "predicate", Sources: cli.EnvVars("PREDICATE_PATH"), Usage: "predicate JSON file; for --type=slsaprovenance1 it is generated from the CI env when omitted"},
				},
				signflags.Cosign(signflags.CosignOpts{})...,
			),
			&cli.StringFlag{Name: "builder-id", Sources: cli.EnvVars("BUILDER_ID"), Usage: "override the SLSA provenance builder.id (e.g. an operator's documented KMS builder identity for an isolated L3 attestor); defaults to the CI-derived workflow identity"},
			&cli.StringFlag{Name: flagFlavor, Sources: cli.EnvVars("BUILD_FLAVOR"), Usage: "build variant recorded as externalParameters.flavor (e.g. a ci-builder flavor like \"rust\")"},
			&cli.StringFlag{Name: flagBaseInputID, Sources: cli.EnvVars("BASE_INPUT_ID"), Usage: "sha256 content id of this build's base inputs, recorded as externalParameters.base_input_id (the SLSA-standard home for base lineage)"},
			&cli.StringFlag{Name: "base-ref", Sources: cli.EnvVars("BASE_IMAGE_REF"), Usage: "the base image this was built FROM, recorded as a resolvedDependency annotated role=base-image (requires --base-digest)"},
			&cli.StringFlag{Name: "base-digest", Sources: cli.EnvVars("BASE_IMAGE_DIGEST"), Usage: "sha256 digest of --base-ref (with or without the sha256: prefix)"},
			&cli.BoolFlag{Name: flagRecursive, Value: true, Sources: cli.EnvVars("ATTEST_RECURSIVE"), Usage: "also attest each per-arch child of a manifest list (one provenance for the whole release). Default true."},
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			image := cmd.Args().First()
			if image == "" {
				return fmt.Errorf("container attest: image reference is required (registry/image@sha256:...): %w", errs.ErrMissingInput)
			}

			method, err := domainrelease.ParseSignMethod(cmd.String("method"))
			if err != nil {
				return err
			}

			prov := provenanceFromEnv(image)
			if bid := cmd.String("builder-id"); bid != "" {
				prov.BuilderID = bid
			}

			prov.Flavor = cmd.String(flagFlavor)
			prov.BaseInputID = cmd.String(flagBaseInputID)

			baseRef, baseDigest := cmd.String("base-ref"), cmd.String("base-digest")
			if (baseRef == "") != (baseDigest == "") {
				return fmt.Errorf("container attest: --base-ref and --base-digest must be set together: %w", errs.ErrUsage)
			}

			if baseRef != "" {
				prov.ResolvedDeps = append(prov.ResolvedDeps, provenance.BaseImageDependency(baseRef, baseDigest))
			}

			return appcontainer.AttestImage(ctx, cosign.New(), os.Stderr, appcontainer.AttestImageInput{
				Image:         image,
				Method:        method,
				PredicateType: cmd.String("type"),
				PredicatePath: cmd.String("predicate"),
				Provenance:    prov,
				Recursive:     cmd.Bool(flagRecursive),
				KeyRef:        cmd.String("key"),
				OIDCIssuer:    cmd.String("oidc-issuer"),
			})
		},
	}
}

// provenanceFromEnv derives the forge-neutral SLSA provenance Input from the
// active forge's CI environment. GitHub and Forgejo expose GITHUB_*; GitLab
// exposes CI_*. Only used when a slsaprovenance predicate is generated. The
// resulting predicate is the same shape `release provenance` emits for blobs.
func provenanceFromEnv(image string) provenance.Input {
	get := os.Getenv
	server := strings.TrimSuffix(cmp.Or(get("GITHUB_SERVER_URL"), get("CI_SERVER_URL")), "/")
	repo := cmp.Or(get("GITHUB_REPOSITORY"), get("CI_PROJECT_PATH"))
	// Short ref name (v1.2.3), not the full GITHUB_REF (refs/tags/v1.2.3), to
	// match the provider's RefName + GitLab's CI_COMMIT_REF_NAME + the release
	// provenance path — one ref representation across forges and artifacts.
	ref := cmp.Or(get("GITHUB_REF_NAME"), get("CI_COMMIT_REF_NAME"))
	sha := cmp.Or(get("GITHUB_SHA"), get("CI_COMMIT_SHA"))

	var source, repoURL string

	if server != "" && repo != "" {
		repoURL = server + "/" + repo
		source = "git+" + repoURL
	}

	var deps []provenance.Dependency
	if repoURL != "" && sha != "" {
		deps = []provenance.Dependency{provenance.SourceDependency(repoURL, ref, sha)}
	}

	// Reproducible build timestamp: $SOURCE_DATE_EPOCH (the same value BuildKit
	// clamps the image to), shared with the release provenance path. Optional —
	// omitted when unset rather than failing the attestation.
	epochTime, _ := cienv.SourceDateEpochRFC3339()

	return provenance.Input{
		BuildType:    provenance.ContainerBuildType,
		BuilderID:    cienv.ProvenanceBuilderID(),
		SourceURI:    source,
		Ref:          ref,
		ImageName:    image,
		InvocationID: cienv.ProvenanceInvocationID(),
		StartedOn:    cmp.Or(get("BUILD_STARTED_ON"), epochTime),
		FinishedOn:   cmp.Or(get("BUILD_FINISHED_ON"), epochTime),
		ResolvedDeps: deps,
	}
}
