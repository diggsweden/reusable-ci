// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	adapterdocker "github.com/diggsweden/reusable-ci/internal/adapters/docker"
	appcontainer "github.com/diggsweden/reusable-ci/internal/app/container"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	domaincontainer "github.com/diggsweden/reusable-ci/internal/domain/container"
)

// manifestGroup wires `reusable-ci container manifest <verb>` —
// container manifest-list operations: merge per-platform digests
// into a single manifest, or inspect an existing one.
func manifestGroup() *cli.Command {
	return &cli.Command{
		Name:  "manifest",
		Usage: "merge / inspect multi-platform container manifest lists",
		Commands: []*cli.Command{
			manifestMergeCmd(),
			manifestInspectCmd(),
		},
	}
}

func manifestMergeCmd() *cli.Command {
	return &cli.Command{
		Name:  "merge",
		Usage: "create a manifest list from digest marker files and tags",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "image-name", Sources: cli.EnvVars("IMAGE_NAME"), Usage: "base image name (without tag) the manifest list points to"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "tags", Sources: cli.EnvVars("TAGS"), Usage: "newline-separated tags to publish for the manifest list"},
			&cli.StringFlag{Name: "digests-dir", Value: domaincontainer.DefaultDigestsDir, Sources: cli.EnvVars("DIGESTS_DIR"), Usage: "directory holding the per-arch digest marker files"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appcontainer.MergeManifest(ctx, adapterdocker.New(), os.Stderr, os.Stderr, appcontainer.MergeManifestInput{
				ImageName:  cmd.String("image-name"),
				Tags:       cmd.String("tags"),
				DigestsDir: cmd.String("digests-dir"),
			})
		},
	}
}

func manifestInspectCmd() *cli.Command {
	return &cli.Command{
		Name:  "inspect",
		Usage: "inspect a pushed manifest list and emit image/digest outputs",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "image", Sources: cli.EnvVars("CONTAINER_IMAGE"), Usage: "fully-qualified image reference (registry/owner/name:tag) to inspect"},
			&cli.StringFlag{Name: "tags", Sources: cli.EnvVars("TAGS"), Usage: "newline-separated tags whose digest output is emitted to the sink"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				_, err := appcontainer.InspectManifest(ctx, adapterdocker.New(), d.OutputSink, os.Stderr, os.Stderr, appcontainer.InspectManifestInput{
					Image: cmd.String("image"),
					Tags:  cmd.String("tags"),
				})

				return err
			})
		},
	}
}
