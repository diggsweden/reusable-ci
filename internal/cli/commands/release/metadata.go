// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	apprelease "github.com/diggsweden/reusable-ci/internal/app/release"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
)

func resolveMetadataCmd() *cli.Command {
	return &cli.Command{
		Name:  "resolve-release-metadata",
		Usage: "compute version / version-no-v / project-name from the release inputs",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "version", Required: true, Sources: cli.EnvVars("VERSION")},
			&cli.StringFlag{Name: "repository", Required: true, Sources: cli.EnvVars("REPOSITORY")},
			&cli.StringFlag{Name: "artifact-name", Sources: cli.EnvVars("ARTIFACT_NAME")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			return apprelease.ResolveMetadata(ctx, d.OutputSink, os.Stderr, apprelease.ResolveMetadataInput{
				Version:      cmd.String("version"),
				Repository:   cmd.String("repository"),
				ArtifactName: cmd.String("artifact-name"),
			})
		},
	}
}
