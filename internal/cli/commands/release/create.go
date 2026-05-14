// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/localfs"
	apprelease "github.com/diggsweden/reusable-ci/internal/app/release"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	domainrelease "github.com/diggsweden/reusable-ci/internal/domain/release"
)

func createCmd() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "create a release on the detected platform with assembled assets",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "tag", Required: true, Sources: cli.EnvVars("TAG_NAME")},
			&cli.StringFlag{Name: "repository", Required: true, Sources: cli.EnvVars("REPOSITORY")},
			&cli.StringFlag{Name: "release-name", Sources: cli.EnvVars("RELEASE_NAME")},
			&cli.BoolFlag{Name: "draft", Sources: cli.EnvVars("DRAFT")},
			&cli.BoolFlag{Name: "make-latest", Value: true, Sources: cli.EnvVars("MAKE_LATEST")},
			&cli.StringFlag{Name: "attach-artifacts", Sources: cli.EnvVars("ATTACH_ARTIFACTS")},
			&cli.StringFlag{Name: "release-notes-file", Value: domainrelease.DefaultReleaseNotesFile, Sources: cli.EnvVars("RELEASE_NOTES_FILE")},
			&cli.StringFlag{Name: "artifact-name", Sources: cli.EnvVars("ARTIFACT_NAME")},
			&cli.StringFlag{Name: "checksums-file", Value: domainrelease.ChecksumsFile, Sources: cli.EnvVars("CI_CHECKSUMS_FILE")},
			&cli.StringFlag{Name: "release-dir", Value: domainrelease.DefaultReleaseArtifactsDir, Sources: cli.EnvVars("RELEASE_DIR")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			return apprelease.CreateRelease(ctx, d.Provider, localfs.New(), os.Stdout, apprelease.CreateReleaseInput{
				Tag:              cmd.String("tag"),
				Repository:       cmd.String("repository"),
				ReleaseName:      cmd.String("release-name"),
				Draft:            cmd.Bool("draft"),
				MakeLatest:       cmd.Bool("make-latest"),
				AttachArtifacts:  cmd.String("attach-artifacts"),
				ReleaseNotesFile: cmd.String("release-notes-file"),
				ArtifactName:     cmd.String("artifact-name"),
				ChecksumsFile:    cmd.String("checksums-file"),
				ReleaseDir:       cmd.String("release-dir"),
			})
		},
	}
}
