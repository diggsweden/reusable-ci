// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
)

// resolveGroup wires `reusable-ci release resolve <verb>` — name and
// metadata resolution helpers used by downstream release-flow steps
// to discover canonical artifact names, versions, and project paths.
func resolveGroup() *cli.Command {
	return &cli.Command{
		Name:  "resolve",
		Usage: "compute canonical artifact names / release metadata",
		Commands: []*cli.Command{
			resolveArtifactNameCmd(),
			resolveMetadataCmd(),
		},
	}
}

func resolveArtifactNameCmd() *cli.Command {
	return &cli.Command{
		Name:  flagArtifactName,
		Usage: "print the canonical upload-artifact name pair for a project type",
		Description: `EXAMPLE:
   reusable-ci release resolve artifact-name --project-type maven`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "project-type",
				Required: true,
				Sources:  cli.EnvVars("PROJECT_TYPE"),
				Usage:    "ecosystem driving the artifact naming (maven/gradle/npm/go/cargo/…)",
			},
			&cli.StringFlag{
				Name:    flagArtifactName,
				Sources: cli.EnvVars("ARTIFACT_NAME"),
				Usage:   "override (gradle / go / cargo only)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			format, err := deps.OutputFormat(cmd)
			if err != nil {
				return err
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return apprelease.ResolveArtifactNames(ctx, os.Stderr, apprelease.ResolveArtifactNamesInput{
					ProjectType:  cmd.String("project-type"),
					ArtifactName: cmd.String(flagArtifactName),
					Format:       format,
					Sink:         d.OutputSink,
				})
			})
		},
	}
}

func resolveMetadataCmd() *cli.Command {
	return &cli.Command{
		Name:  "metadata",
		Usage: "compute version / version-no-v / project-name from the release inputs",
		Description: `EXAMPLE:
   reusable-ci release resolve metadata --version v1.2.3 --repository org/app`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagVersion, Required: true, Sources: cli.EnvVars("VERSION"), Usage: "release version (e.g. v1.2.3 or 1.2.3)"},
			&cli.StringFlag{Name: flagRepository, Required: true, Sources: cienv.Repository(), Usage: "\"owner/repo\" slug used to derive the default project name"},
			&cli.StringFlag{Name: flagArtifactName, Sources: cli.EnvVars("ARTIFACT_NAME"), Usage: "explicit project name override (skips the repo-basename heuristic)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return apprelease.ResolveMetadata(ctx, d.OutputSink, os.Stderr, apprelease.ResolveMetadataInput{
					Version:      cmd.String(flagVersion),
					Repository:   cmd.String(flagRepository),
					ArtifactName: cmd.String(flagArtifactName),
				})
			})
		},
	}
}
