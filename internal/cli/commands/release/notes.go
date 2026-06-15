// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

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
			&cli.StringFlag{Name: "source-file", Value: "ReleasenotesTmp", Sources: cli.EnvVars("SOURCE_FILE"), Usage: "path to the git-cliff–generated changelog used as the source body"},
			&cli.StringFlag{Name: "target-file", Value: domainrelease.DefaultReleaseNotesFile, Sources: cli.EnvVars("TARGET_FILE"), Usage: "destination path for the assembled release-notes body (\"-\" writes to stdout)"},
			&cli.StringFlag{Name: "release-version", Sources: cli.EnvVars("RELEASE_VERSION"), Usage: "release version used in the fallback header when no source file is found"},
			&cli.StringFlag{Name: "release-commit", Sources: cli.EnvVars("RELEASE_COMMIT"), Usage: "release commit SHA used in the fallback body"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return apprelease.PrepareNotes(ctx, os.Stderr, apprelease.PrepareNotesInput{
				SourceFile:     cmd.String("source-file"),
				TargetFile:     cmd.String("target-file"),
				ReleaseVersion: cmd.String("release-version"),
				ReleaseCommit:  cmd.String("release-commit"),
			})
		},
	}
}

func verifyChangelogCmd() *cli.Command {
	return &cli.Command{
		Name:  "verify-changelog",
		Usage: "verify the generated changelog artifact for this release exists and print a preview",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "changelog-file",
				Required: true,
				Sources:  cli.EnvVars("CHANGELOG_FILE"),
				Usage:    "path to the generated changelog file to verify",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return apprelease.VerifyChangelog(ctx, os.Stderr, apprelease.VerifyChangelogInput{
				ChangelogFile: cmd.String("changelog-file"),
			})
		},
	}
}
