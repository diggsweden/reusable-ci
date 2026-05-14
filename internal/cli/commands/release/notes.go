// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	apprelease "github.com/diggsweden/reusable-ci/internal/app/release"
	domainrelease "github.com/diggsweden/reusable-ci/internal/domain/release"
)

func notesCmd() *cli.Command {
	return &cli.Command{
		Name:  "notes",
		Usage: "prepare release-notes file from changelog artifact, fall back to a stub when missing",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "source-file", Value: "ReleasenotesTmp", Sources: cli.EnvVars("SOURCE_FILE")},
			&cli.StringFlag{Name: "target-file", Value: domainrelease.DefaultReleaseNotesFile, Sources: cli.EnvVars("TARGET_FILE")},
			&cli.StringFlag{Name: "release-version", Sources: cli.EnvVars("RELEASE_VERSION")},
			&cli.StringFlag{Name: "release-commit", Sources: cli.EnvVars("RELEASE_COMMIT")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return apprelease.PrepareNotes(ctx, os.Stdout, apprelease.PrepareNotesInput{
				SourceFile:     cmd.String("source-file"),
				TargetFile:     cmd.String("target-file"),
				ReleaseVersion: cmd.String("release-version"),
				ReleaseCommit:  cmd.String("release-commit"),
			})
		},
	}
}

func validateChangelogCmd() *cli.Command {
	return &cli.Command{
		Name:  "validate-changelog",
		Usage: "verify a generated changelog file exists and print a preview",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "changelog-file",
				Required: true,
				Sources:  cli.EnvVars("CHANGELOG_FILE"),
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return apprelease.ValidateChangelog(ctx, os.Stdout, apprelease.ValidateChangelogInput{
				ChangelogFile: cmd.String("changelog-file"),
			})
		},
	}
}
