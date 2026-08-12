// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	appbaseimages "github.com/diggsweden/reusable-ci/v3/internal/app/baseimages"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/regflags"
	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
)

const (
	flagReleaseImages = "release-images"
	flagMaxDelete     = "max-delete"
	flagDryRun        = "dry-run"
)

func baseImagesPruneCmd() *cli.Command {
	return &cli.Command{
		Name:  "prune",
		Usage: "delete promoted base images no supported release is built on",
		Description: `Retention for the base images a consumer's release images are built FROM.
Every base image whose base-input ID appears in no supported release's verified
SLSA attestation is deleted; everything else is left alone.

The keep-set comes from attestations rather than from ledgers on purpose: the
release-image ledger is internal evidence and never published, while the
predicate recording base.input_id is signed and pushed alongside each release
image. An attestation that does not verify, or that names no base, aborts the
run — under-counting the keep-set would delete a base a supported release
depends on.

Base images are content-addressed, so pruning cannot break a future build: a
build derives its own base-input ID and either finds that image or rebuilds it.
A wrong deletion costs a rebuild, not a broken release.

Deletion goes through the forge package/registry API by tag, never by manifest
digest, because several tags can share one manifest.

--dry-run is the default: run it, read the list, then pass --dry-run=false.

EXAMPLE:
   reusable-ci container base-images prune \
     --repository-suffix -base \
     --release-images "$(cat supported-release-images.txt)" \
     --cosign-public-key-path keys/cosign.pub \
     --dry-run=false`,
		Flags: append(append(baseImagesRepositoryFlags(), baseImagesPublicKeyFlags()...),
			&cli.StringFlag{
				Name:    flagReleaseImages,
				Sources: cli.EnvVars("RELEASE_IMAGES"),
				Usage:   "newline- or comma-separated digest-pinned release image refs whose bases must be kept",
			},
			&cli.IntFlag{
				Name:    flagMaxDelete,
				Value:   20,
				Sources: cli.EnvVars("BASE_IMAGES_MAX_DELETE"),
				Usage:   "refuse the run when more than this many base images are unreferenced (0 disables the bound)",
			},
			&cli.BoolFlag{
				Name:    flagDryRun,
				Value:   true,
				Sources: cli.EnvVars("BASE_IMAGES_PRUNE_DRY_RUN"),
				Usage:   "report what would be deleted without deleting it",
			},
			regflags.AuthFile(regflags.AuthFileOpts{Usage: "registry auth file for attestation verification"}),
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			common, err := baseImagesCommonFromCmd(cmd, false, true)
			if err != nil {
				return err
			}

			// Same two package-API roles base-image cleanup drives, resolved the
			// same way: any forge implementing both is supported, and the rest
			// get the typed "unsupported" refusal instead of a partial prune.
			forge, err := deps.ProviderWithServerURL(common.ServerURL)
			if err != nil {
				return err
			}

			forgeProvider, err := deps.RoleFrom[baseImageStagingCleaner](forge, "base-image retention")
			if err != nil {
				return err
			}

			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				result, err := appbaseimages.PruneBaseImages(ctx,
					forgeProvider,
					cosign.NewIsolated("DOCKER_CONFIG"),
					os.Stderr,
					appbaseimages.BaseImagePruneInput{
						ExpectedRepository: common.ExpectedRepository,
						ReleaseImages:      splitReleaseImages(cmd.String(flagReleaseImages)),
						Attestation: domaincontainer.AttestationVerifyRequest{
							PredicateType: domaincontainer.PredicateTypeSLSAProvenance1,
							KeyRef:        common.PublicKeyPath,
						},
						MaxDelete: cmd.Int(flagMaxDelete),
						DryRun:    cmd.Bool(flagDryRun),
					})
				if err != nil {
					return err
				}

				return writeBaseImagesPruneOutputs(ctx, dep, result)
			})
		},
	}
}

// splitReleaseImages accepts refs one per line or comma-separated, so a caller
// can pass a here-doc, a file, or a workflow output without reshaping it.
func splitReleaseImages(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ' ' || r == '\t'
	})

	refs := make([]string, 0, len(fields))

	for _, field := range fields {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			refs = append(refs, trimmed)
		}
	}

	return refs
}

// writeBaseImagesPruneOutputs publishes the counts a scheduled job wants to
// alert on, and the pruned IDs so a run is auditable from its logs alone.
func writeBaseImagesPruneOutputs(ctx context.Context, dep *deps.Deps, result appbaseimages.BaseImagePruneResult) error {
	if err := dep.OutputSink.Set(ctx, "inventory_count", strconv.Itoa(len(result.Inventory))); err != nil {
		return err
	}

	if err := dep.OutputSink.Set(ctx, "referenced_count", strconv.Itoa(len(result.Referenced))); err != nil {
		return err
	}

	if err := dep.OutputSink.Set(ctx, "prunable_count", strconv.Itoa(len(result.Prunable))); err != nil {
		return err
	}

	if err := dep.OutputSink.Set(ctx, "deleted_count", strconv.Itoa(len(result.Deleted))); err != nil {
		return err
	}

	return dep.OutputSink.Set(ctx, "pruned_base_input_ids", strings.Join(result.Deleted, ","))
}
