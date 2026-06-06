// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	apprelease "github.com/diggsweden/reusable-ci/internal/app/release"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	domainrelease "github.com/diggsweden/reusable-ci/internal/domain/release"
)

// attachmentsGroup wires `reusable-ci release attachments <verb>` —
// the release-attachment lifecycle. Two verbs: `plan` (resolve the
// attach-artifacts globs into a concrete file list) and `upload`
// (push the planned files to the release).
func attachmentsGroup() *cli.Command {
	return &cli.Command{
		Name:  "attachments",
		Usage: "plan and upload release attachments",
		Commands: []*cli.Command{
			attachmentsPlanCmd(),
			attachmentsUploadCmd(),
		},
	}
}

func attachmentsPlanCmd() *cli.Command {
	return &cli.Command{
		Name:  "plan",
		Usage: "emit effective release attachment globs including extracted binaries",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "user-attach", Sources: cli.EnvVars("USER_ATTACH"), Usage: "comma-separated globs the workflow explicitly asks to attach"},
			&cli.StringFlag{Name: "binaries-dir", Value: domainrelease.DefaultReleaseBinariesDir, Sources: cli.EnvVars("BINARIES_DIR"), Usage: "directory holding extracted multi-arch binaries (probed for presence)"},
			&cli.StringFlag{Name: "binaries-glob", Value: domainrelease.DefaultReleaseBinariesGlob, Sources: cli.EnvVars("BINARIES_GLOB"), Usage: "glob auto-attached when binaries-dir is non-empty"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				_, err := apprelease.AttachArtifacts(ctx, d.OutputSink, os.Stderr, apprelease.AttachArtifactsInput{
					UserAttach:   cmd.String("user-attach"),
					BinariesDir:  cmd.String("binaries-dir"),
					BinariesGlob: cmd.String("binaries-glob"),
				})

				return err
			})
		},
	}
}

func attachmentsUploadCmd() *cli.Command {
	return &cli.Command{
		Name:  "upload",
		Usage: "expand a glob pattern and attach matching files to the platform release (gh release upload --clobber)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "tag", Required: true, Sources: cli.EnvVars("REF_NAME", "TAG_NAME"), Usage: "tag of the existing release to attach to (e.g. v1.2.3)"},
			&cli.StringFlag{Name: "pattern", Sources: cli.EnvVars("ATTACH_PATTERN"), Usage: "comma-separated globs to expand and upload"},
			&cli.StringFlag{Name: "working-dir", Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "directory the globs are resolved relative to"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)

				up, err := d.RequireReleaseAssetUploader()
				if err != nil {
					return err
				}

				return apprelease.UploadAttachments(ctx, up, os.Stderr, annot, apprelease.UploadAttachmentsInput{
					Tag:        cmd.String("tag"),
					Pattern:    cmd.String("pattern"),
					WorkingDir: cmd.String("working-dir"),
				})
			})
		},
	}
}
