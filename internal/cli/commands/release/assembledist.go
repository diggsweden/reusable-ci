// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/urfave/cli/v3"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/planfile"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// planScopeAssembleDist is the plan-file scope of `release assemble-dist`
// in $REUSABLE_CI_PLAN (flag > plan > env > default).
const planScopeAssembleDist = "release assemble-dist"

func assembleDistCmd() *cli.Command {
	return &cli.Command{
		Name:  "assemble-dist",
		Usage: "assemble a release dist/ hand-off from run artifacts and image ledgers",
		Description: `Downloads named current-run artifacts or a typed artifact-transfer plan,
optionally merges per-image release ledgers into dist/release-images.json,
prunes top-level directories on request, computes the canonical release hand-off
digest, and writes a digest output for the signing job. Every flag may
also be fed from the $REUSABLE_CI_PLAN plan file under the
"release assemble-dist" scope (flag > plan > env > default).

EXAMPLE:
   reusable-ci release assemble-dist --artifact-names build-a --path dist/`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagArtifactTransferPlanJSON, Sources: planfile.Vars(planScopeAssembleDist, flagArtifactTransferPlanJSON, "ARTIFACT_TRANSFER_PLAN_JSON"), Usage: "optional typed artifact-transfer plan JSON; mutually exclusive with --artifact-names"},
			&cli.StringFlag{Name: "artifact-names", Sources: planfile.Vars(planScopeAssembleDist, "artifact-names", "ARTIFACT_NAMES"), Usage: "newline-separated current-run artifact names to download into --path"},
			&cli.StringFlag{Name: "path", Value: defaultDistPath, Sources: planfile.Vars(planScopeAssembleDist, "path", "DIST_DIR"), Usage: "directory to assemble and digest"},
			&cli.StringFlag{Name: "ledger-files", Sources: planfile.Vars(planScopeAssembleDist, "ledger-files", "LEDGER_FILES"), Usage: "newline-separated ledger files or directories to merge after artifact download"},
			&cli.IntFlag{Name: "ledger-expected-count", Sources: planfile.Vars(planScopeAssembleDist, "ledger-expected-count", "LEDGER_EXPECTED_COUNT"), Usage: "when greater than zero, require the merged release image ledger to contain exactly this many entries"},
			&cli.StringFlag{Name: "release-images-path", Value: "dist/release-images.json", Sources: planfile.Vars(planScopeAssembleDist, "release-images-path", "RELEASE_IMAGES_PATH"), Usage: "merged release image ledger path"},
			&cli.StringFlag{Name: "prune-dirs", Value: "false", Sources: planfile.Vars(planScopeAssembleDist, "prune-dirs", "PRUNE_DIRS"), Usage: "whether to remove top-level directories before digesting: true|false"}, //nolint:goconst // generic bool literal, not a shared identifier.
			&cli.StringFlag{Name: "run-id", Sources: planfile.Chain(planScopeAssembleDist, "run-id", cienv.RunID()), Usage: "run the artifact belongs to (default: current run)"},
			&cli.StringFlag{Name: "repository", Sources: planfile.Chain(planScopeAssembleDist, "repository", cienv.Repository()), Usage: "owner/repo the run belongs to (default: current repo)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			pruneDirs, err := parseStrictBoolFlag(cmd.String("prune-dirs"), "prune-dirs")
			if err != nil {
				return err
			}

			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				dl, err := dep.RequireRunArtifactDownloader()
				if err != nil {
					return err
				}

				_, err = apprelease.AssembleDist(ctx, dl, dep.OutputSink, os.Stderr, apprelease.AssembleDistInput{
					ArtifactNames:            cmd.String("artifact-names"),
					ArtifactTransferPlanJSON: cmd.String(flagArtifactTransferPlanJSON),
					Path:                     cmd.String("path"),
					LedgerFiles:              cmd.String("ledger-files"),
					LedgerExpectedCount:      cmd.Int("ledger-expected-count"),
					ReleaseImagesPath:        cmd.String("release-images-path"),
					PruneDirs:                pruneDirs,
					RunID:                    cmd.String("run-id"),
					Repository:               cmd.String("repository"),
				})

				return err
			})
		},
	}
}

func parseStrictBoolFlag(raw, name string) (bool, error) {
	switch raw {
	case "true", "false":
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return false, err
		}

		return parsed, nil
	default:
		return false, fmt.Errorf("%s must be true or false: %w", name, errs.ErrUsage)
	}
}
