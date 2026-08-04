// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ociregistry"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/syft"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/signflags"
	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

func ledgerSignCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdSign,
		Usage: "sign and attest every release image recorded in the ledger",
		Description: `Signer-side release-image loop: validates the digest-first ledger,
resolves each image by candidate_tag or digest ref, cosign-signs the immutable
digest, generates a CycloneDX image SBOM with syft, attests that SBOM, enriches
the release SLSA predicate with per-image/base-lineage fields, and attests it.

This is the reusable-ci replacement for forgejo-ci's sign-promote-images.sh sign
step; promotion remains a separate ledger promote operation.`,
		Flags: append(
			[]cli.Flag{
				ledgerPathFlag(),
				releaseTagFlag(),
				ledgerAuthFileFlag("registry auth file for resolving each image before signing"),
				&cli.StringFlag{Name: "provenance-predicate", Value: "dist/slsa-provenance.predicate.json", Sources: cli.EnvVars("SLSA_PROVENANCE_PREDICATE"), Usage: "base SLSA provenance predicate JSON enriched per image before attestation"},
				&cli.StringFlag{Name: flagProvenanceEnvelope, Sources: cli.EnvVars("SLSA_PROVENANCE_ENVELOPE"), Usage: "in-toto statement JSON; its .predicate is enriched per image before attestation"},
				&cli.BoolFlag{Name: flagRecursive, Sources: cli.EnvVars("LEDGER_SIGN_RECURSIVE"), Usage: "pass --recursive to cosign sign/attest for manifest-list children (default false to match forgejo-ci signer behavior)"},
				&cli.StringFlag{Name: flagExpectedImageRepository, Sources: cli.EnvVars("LEDGER_SIGN_EXPECTED_IMAGE_REPOSITORY", "LEDGER_EXPECTED_IMAGE_REPOSITORY"), Usage: "optional exact image repository allowed for ref, final_tag, moving_tag, and candidate_tag (for forge-specific signer boundaries)"},
				&cli.StringFlag{Name: flagExpectedBaseRepository, Sources: cli.EnvVars("LEDGER_SIGN_EXPECTED_BASE_REPOSITORY"), Usage: "optional exact base image repository allowed for base_ref"},
				&cli.StringFlag{Name: flagSBOMPathPattern, Sources: cli.EnvVars("LEDGER_SIGN_SBOM_PATH_PATTERN"), Usage: "optional regular expression every ledger SBOM path must match"},
			},
			signflags.Cosign(signflags.CosignOpts{MethodNote: cosignMethodNoteGPG})...,
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			data, err := cliio.ReadFile(cmd.String(flagLedger))
			if err != nil {
				return fmt.Errorf("ledger: read %s: %w", cmd.String(flagLedger), err)
			}

			entries, err := imageledger.Parse(data)
			if err != nil {
				return err
			}

			method, err := domainrelease.ParseSignMethod(cmd.String("method"))
			if err != nil {
				return err
			}

			predicatePath := cmd.String("provenance-predicate")

			predicateEnvelopePath := cmd.String(flagProvenanceEnvelope)
			if predicateEnvelopePath != "" {
				predicatePath = ""
			}

			return runLedgerSign(ctx,
				cosign.New(),
				ledgerRegistry(cmd),
				appcontainer.SignLedgerImagesInput{
					Entries:                 entries,
					ReleaseTag:              cmd.String(flagTag),
					PredicatePath:           predicatePath,
					PredicateEnvelopePath:   predicateEnvelopePath,
					Method:                  method,
					Recursive:               cmd.Bool(flagRecursive),
					KeyRef:                  cmd.String("key"),
					OIDCIssuer:              cmd.String("oidc-issuer"),
					FulcioURL:               signflags.ReadEndpoints(cmd).FulcioURL,
					RekorURL:                signflags.ReadEndpoints(cmd).RekorURL,
					ExpectedImageRepository: cmd.String(flagExpectedImageRepository),
					ExpectedBaseRepository:  cmd.String(flagExpectedBaseRepository),
					SBOMPathPattern:         cmd.String(flagSBOMPathPattern),
				})
		},
	}
}

// runLedgerSign is the shared sign body for `ledger sign` and
// `release-images sign`: hand the validated ledger entries to the
// app-layer signer with the package's syft evidence adapter and stderr
// streams. The caller picks the cosign environment (ambient vs
// signer-isolated) and the digest resolver (ambient vs auth-file).
func runLedgerSign(ctx context.Context, signer *cosign.Adapter, resolver *ociregistry.Adapter, in appcontainer.SignLedgerImagesInput) error {
	return appcontainer.SignLedgerImages(ctx,
		signer,
		&syft.Adapter{UnsetEnv: signerSecretEnv()},
		resolver,
		os.Stderr,
		os.Stderr,
		in)
}

func signerSecretEnv() []string {
	return []string{
		"COSIGN_KEY",
		"COSIGN_PASSWORD",
		"FORGEJO_TOKEN",
		"GPG_SIGNING_FINGERPRINT",
		"GPG_SIGNING_KEY",
		"GPG_SIGNING_PASSWORD",
		"MISE_FORGEJO_TOKEN",
		"MISE_GITHUB_TOKEN",
		"REUSABLE_CI_PROVIDER_TOKEN",
		"REGISTRY_PASSWORD",
		"REGISTRY_TOKEN",
		envRegistryUser,
	}
}
