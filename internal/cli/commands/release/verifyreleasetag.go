// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
)

// verifyTagCmd is the Go port of forgejo-ci's verify-release-tag.sh: a
// trust-boundary re-check that the checkout, the published remote tag,
// and "this is still the latest release" all hold before signing —
// refusing to publish a stale or hijacked release.
func verifyTagCmd() *cli.Command {
	return &cli.Command{
		Name:  "verify-tag",
		Usage: "re-verify the release tag against the remote: checkout==release-sha, tag points to it, and not superseded",
		Description: `EXAMPLE:
   reusable-ci release verify-tag --tag v1.2.3 --release-sha abc... --repo-url https://github.com/org/app`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagReleaseSHA, Sources: cli.EnvVars("RELEASE_SHA"), Usage: "commit the release is built from"},
			&cli.StringFlag{Name: flagTag, Sources: cienv.Tag(), Usage: "release tag (e.g. v1.2.3)"},
			&cli.StringFlag{Name: "repo-url", Sources: cli.EnvVars("REPO_URL"), Usage: "remote repository URL for ls-remote"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return apprelease.VerifyReleaseTag(ctx, git.New(), os.Stderr, apprelease.VerifyReleaseTagInput{
				ReleaseSHA: cmd.String(flagReleaseSHA),
				Tag:        cmd.String(flagTag),
				RepoURL:    cmd.String("repo-url"),
			})
		},
	}
}
