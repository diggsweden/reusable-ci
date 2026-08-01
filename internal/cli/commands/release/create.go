// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/osfs"
	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

func createCmd() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "create a release on the detected platform with assembled assets",
		Description: `EXAMPLES:
   # Create a stable release from a tag (assets from ./release-artifacts and checksums.sha256)
   reusable-ci release create --tag=v1.2.3 --repository=examplescope/myapp

   # Create a draft with custom release notes and extra attachments
   reusable-ci release create --tag=v1.2.3 --repository=examplescope/myapp \
       --draft --release-notes-file=NOTES.md --attach-artifacts="dist/*.tar.gz,dist/*.bundle"`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagTag, Required: true, Sources: cienv.Tag(), Usage: "tag the release is created from (e.g. v1.2.3)"},
			&cli.StringFlag{Name: flagRepository, Required: true, Sources: cienv.Repository(), Usage: "\"owner/repo\" on GitHub; \"group/project[/sub]\" on GitLab"},
			&cli.StringFlag{Name: "release-name", Sources: cli.EnvVars("RELEASE_NAME"), Usage: "human-readable release title (defaults to the tag)"},
			&cli.BoolFlag{Name: "draft", Sources: cli.EnvVars("DRAFT"), Usage: "create the release as a draft (not published until edited)"},
			&cli.StringFlag{Name: "make-latest", Value: "true", Sources: cli.EnvVars("MAKE_LATEST"), Usage: "platform latest handling: true, false, or legacy"},
			&cli.StringFlag{Name: flagAttachArtifacts, Sources: cli.EnvVars("ATTACH_ARTIFACTS"), Usage: "comma-separated globs of extra files to attach beyond release-dir"},
			&cli.StringFlag{Name: "release-notes-file", Value: domainrelease.DefaultReleaseNotesFile, Sources: cli.EnvVars("RELEASE_NOTES_FILE"), Usage: "path to the release-notes markdown body"},
			&cli.StringFlag{Name: flagArtifactName, Sources: cli.EnvVars("ARTIFACT_NAME"), Usage: "project slug used in computed asset names (defaults to repo basename)"},
			&cli.StringFlag{Name: "checksums-file", Value: domainrelease.ChecksumsFile, Sources: cli.EnvVars("CI_CHECKSUMS_FILE"), Usage: "path to the SHA256 manifest to attach"},
			&cli.StringFlag{Name: "release-dir", Value: domainrelease.DefaultReleaseArtifactsDir, Sources: cli.EnvVars("RELEASE_DIR"), Usage: "directory whose files are attached as release assets"},
			&cli.StringFlag{Name: flagAssembly, Sources: cli.EnvVars("RELEASE_ASSEMBLY"), Usage: "release assembly manifest to upload exactly"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				rc, err := d.RequireReleaseCreator()
				if err != nil {
					return err
				}

				return apprelease.CreateRelease(ctx, rc, osfs.New(), os.Stderr, apprelease.CreateReleaseInput{
					Tag:              cmd.String(flagTag),
					Repository:       cmd.String(flagRepository),
					ReleaseName:      cmd.String("release-name"),
					Draft:            cmd.Bool("draft"),
					MakeLatest:       cmd.String("make-latest"),
					AttachArtifacts:  cmd.String(flagAttachArtifacts),
					ReleaseNotesFile: cmd.String("release-notes-file"),
					ArtifactName:     cmd.String(flagArtifactName),
					ChecksumsFile:    cmd.String("checksums-file"),
					ReleaseDir:       cmd.String("release-dir"),
					AssemblyFile:     cmd.String(flagAssembly),
				})
			})
		},
	}
}
