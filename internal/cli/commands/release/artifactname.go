// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	apprelease "github.com/diggsweden/reusable-ci/internal/app/release"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

func resolveArtifactNameCmd() *cli.Command {
	return &cli.Command{
		Name:      "resolve-artifact-name",
		Usage:     "print the canonical upload-artifact name pair for a project type",
		ArgsUsage: "<project-type>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "artifact-name",
				Sources: cli.EnvVars("ARTIFACT_NAME"),
				Usage:   "override (gradle / cargo only)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) < 1 {
				return fmt.Errorf("Usage: resolve-artifact-name <project-type>: %w", errs.ErrUsage)
			}
			return apprelease.ResolveArtifactNames(ctx, os.Stdout, apprelease.ResolveArtifactNamesInput{
				ProjectType:  args[0],
				ArtifactName: cmd.String("artifact-name"),
			})
		},
	}
}
