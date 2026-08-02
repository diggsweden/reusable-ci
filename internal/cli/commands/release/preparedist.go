// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
)

func prepareDistCmd() *cli.Command {
	return &cli.Command{
		Name:  "prepare-dist",
		Usage: "validate/prune a dist hand-off, compute its digest, and emit digest output before artifact upload",
		Description: `Validates and prepares the unsigned dist/ hand-off before upload:
optionally prunes top-level directories, computes the canonical release dist
digest, writes digest=<sha256> to the CI output sink, and logs the digest.

EXAMPLE:
   reusable-ci release prepare-dist --path dist/ --prune-dirs false`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "path", Value: defaultDistPath, Sources: cli.EnvVars("DIST_DIR", "UPLOAD_DIST_PATH"), Usage: "directory to prepare for upload"},
			&cli.StringFlag{Name: "prune-dirs", Value: "false", Sources: cli.EnvVars("PRUNE_DIRS", "UPLOAD_DIST_PRUNE"), Usage: "whether to remove top-level directories before digesting: true|false"}, //nolint:goconst // generic bool literal, not a shared identifier.
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			pruneDirs, err := parseStrictBoolFlag(cmd.String("prune-dirs"), "prune-dirs")
			if err != nil {
				return err
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				_, err := apprelease.PrepareDist(ctx, d.OutputSink, os.Stderr, apprelease.PrepareDistInput{
					Path:      cmd.String("path"),
					PruneDirs: pruneDirs,
				})

				return err
			})
		},
	}
}
