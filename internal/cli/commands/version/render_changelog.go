// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/changelog"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
)

func renderChangelogCmd() *cli.Command {
	return &cli.Command{
		Name:  "render-changelog",
		Usage: "render CHANGELOG.md and commit-msg.txt with git-chglog or git-cliff, preserving same-version recovery",
		Description: `EXAMPLE:
   reusable-ci version render-changelog --backend git-chglog --tag v1.2.3 \
     --changelog-config .chglog/config-keepachangelog.yml \
     --commit-body-config .chglog/config-minimal.yml`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "backend", Value: "git-chglog", Sources: cli.EnvVars("CHANGELOG_BACKEND"), Usage: "changelog renderer backend: git-chglog or git-cliff"},
			&cli.StringFlag{Name: flagTag, Required: true, Sources: cli.EnvVars("RELEASE_TAG", "TAG_NAME"), Usage: "final stable release tag (vMAJOR.MINOR.PATCH)"},
			&cli.StringFlag{Name: "changelog-config", Required: true, Sources: cli.EnvVars("CHANGELOG_CONFIG"), Usage: "renderer config for CHANGELOG.md"},
			&cli.StringFlag{Name: "commit-body-config", Required: true, Sources: cli.EnvVars("COMMIT_BODY_CONFIG"), Usage: "renderer config for the release bump commit body"},
			&cli.StringFlag{Name: "commit-trailers", Sources: cli.EnvVars("COMMIT_TRAILERS"), Usage: "commit trailers appended after [skip ci]"},
			&cli.StringFlag{Name: "changelog-path", Value: "CHANGELOG.md", Sources: cli.EnvVars("CHANGELOG_PATH"), Usage: "output changelog path"},
			&cli.StringFlag{Name: "commit-body-path", Value: "commit-body.txt", Sources: cli.EnvVars("COMMIT_BODY_PATH"), Usage: "output commit body path"},
			&cli.StringFlag{Name: "commit-message-file", Value: defaultCommitMessageFile, Sources: cli.EnvVars("COMMIT_MESSAGE_FILE"), Usage: "output commit message path"},
			&cli.StringFlag{Name: "existing-release-sha-file", Value: ".forgejo-ci-existing-release-sha", Sources: cli.EnvVars("EXISTING_RELEASE_SHA_FILE"), Usage: "marker file written when same-version recovery reuses an existing release tag"},
			&cli.StringFlag{Name: "remote", Value: "origin", Sources: cli.EnvVars("RELEASE_REMOTE"), Usage: "git remote to query"},
			&cli.StringFlag{Name: flagBranch, Value: "main", Sources: cli.EnvVars("RELEASE_BRANCH", "BRANCH"), Usage: "branch containing the release bump"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			_, err := appversion.ChangelogRender(ctx, git.New(), changelog.New(), os.Stderr, appversion.ChangelogRenderInput{
				Backend:                cmd.String("backend"),
				Tag:                    cmd.String(flagTag),
				Remote:                 cmd.String("remote"),
				Branch:                 cmd.String(flagBranch),
				ChangelogConfig:        cmd.String("changelog-config"),
				CommitBodyConfig:       cmd.String("commit-body-config"),
				ChangelogPath:          cmd.String("changelog-path"),
				CommitBodyPath:         cmd.String("commit-body-path"),
				CommitMessagePath:      cmd.String("commit-message-file"),
				ExistingReleaseSHAPath: cmd.String("existing-release-sha-file"),
				CommitTrailers:         cmd.String("commit-trailers"),
			})

			return err
		},
	}
}
