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
		Description: `EXAMPLES:
   # Create a stable release from a tag (assets from ./release-artifacts and checksums.sha256)
   reusable-ci release create --tag=v1.2.3 --repository=diggsweden/reusable-ci

   # Create a draft with custom release notes and extra attachments
   reusable-ci release create --tag=v1.2.3 --repository=diggsweden/reusable-ci \
       --draft --release-notes-file=NOTES.md --attach-artifacts="dist/*.tar.gz,dist/*.bundle"`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "tag", Required: true, Sources: cli.EnvVars("TAG_NAME"), Usage: "tag the release is created from (e.g. v1.2.3)"},
			&cli.StringFlag{Name: "repository", Required: true, Sources: cli.EnvVars("REPOSITORY"), Usage: "\"owner/repo\" on GitHub; \"group/project[/sub]\" on GitLab"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "release-name", Sources: cli.EnvVars("RELEASE_NAME"), Usage: "human-readable release title (defaults to the tag)"},
			&cli.BoolFlag{Name: "draft", Sources: cli.EnvVars("DRAFT"), Usage: "create the release as a draft (not published until edited)"},
			&cli.BoolFlag{Name: "make-latest", Value: true, Sources: cli.EnvVars("MAKE_LATEST"), Usage: "mark this release as 'latest' on the platform"},
			&cli.StringFlag{Name: "attach-artifacts", Sources: cli.EnvVars("ATTACH_ARTIFACTS"), Usage: "comma-separated globs of extra files to attach beyond release-dir"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "release-notes-file", Value: domainrelease.DefaultReleaseNotesFile, Sources: cli.EnvVars("RELEASE_NOTES_FILE"), Usage: "path to the release-notes markdown body"},
			&cli.StringFlag{Name: "artifact-name", Sources: cli.EnvVars("ARTIFACT_NAME"), Usage: "project slug used in computed asset names (defaults to repo basename)"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "checksums-file", Value: domainrelease.ChecksumsFile, Sources: cli.EnvVars("CI_CHECKSUMS_FILE"), Usage: "path to the SHA256 manifest to attach"},
			&cli.StringFlag{Name: "release-dir", Value: domainrelease.DefaultReleaseArtifactsDir, Sources: cli.EnvVars("RELEASE_DIR"), Usage: "directory whose files are attached as release assets"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				rc, err := d.RequireReleaseCreator()
				if err != nil {
					return err
				}

				return apprelease.CreateRelease(ctx, rc, localfs.New(), os.Stderr, apprelease.CreateReleaseInput{
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
			})
		},
	}
}
