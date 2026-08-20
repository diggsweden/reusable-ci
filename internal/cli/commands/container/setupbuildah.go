// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/apt"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/buildah"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
)

func setupBuildahCmd() *cli.Command {
	return &cli.Command{
		Name:  "setup-buildah",
		Usage: "install Buildah runtime packages and configure job-local storage",
		Description: `Installs missing Buildah/fuse-overlayfs packages when requested,
writes a job-local containers storage config, prefers overlay/fuse-overlayfs only
after buildah info and an optional layered build probe succeed, and falls back to
vfs. Emits CONTAINERS_STORAGE_CONF and TMPDIR to the runner env file for later CI
steps.

EXAMPLE:
   reusable-ci container setup-buildah --extra-packages "jq skopeo" --summary`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "extra-packages", Sources: cli.EnvVars("SETUP_BUILDAH_EXTRA_PACKAGES"), Usage: "whitespace-separated additional apt packages to install"},
			&cli.BoolFlag{Name: "install-packages", Value: true, Sources: cli.EnvVars("SETUP_BUILDAH_INSTALL_PACKAGES"), Usage: "install missing Buildah/fuse-overlayfs/extra packages with apt-get"},
			&cli.BoolFlag{Name: "probe-build", Value: true, Sources: cli.EnvVars("SETUP_BUILDAH_PROBE_BUILD"), Usage: "validate storage with a layered build probe that writes into a directory its base image owns"},
			&cli.BoolFlag{Name: "print-store", Sources: cli.EnvVars("CONTAINER_STORAGE_PRINT_STORE"), Usage: "print selected buildah storage details after setup"},
			&cli.BoolFlag{Name: "summary", Sources: cli.EnvVars("CONTAINER_STORAGE_SUMMARY"), Usage: "append selected storage details to the step summary"},
			&cli.StringFlag{Name: "storage-conf", Sources: cli.EnvVars("CONTAINERS_STORAGE_CONF"), Usage: "containers storage config path (default: $RUNNER_TEMP/containers-storage.conf)"},
			&cli.StringFlag{Name: "storage-root", Sources: cli.EnvVars("CONTAINER_STORAGE_ROOT"), Usage: "containers storage root path (default: $RUNNER_TEMP/containers-storage)"},
			&cli.StringFlag{Name: "tmp-dir", Sources: cli.EnvVars("CONTAINER_TMPDIR"), Usage: "job-local temp directory (default: $RUNNER_TEMP/container-tmp)"},
			&cli.StringFlag{Name: flagTempDir, Aliases: []string{flagTempDirLegacy}, Sources: cienv.TempDir(), Usage: "scratch directory used for default paths"},
			&cli.StringFlag{Name: "env-file", Sources: cli.EnvVars("FORGEJO_ENV", "GITHUB_ENV"), Usage: "runner env file receiving CONTAINERS_STORAGE_CONF and TMPDIR"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				_, err := appcontainer.SetupBuildah(ctx, buildah.New(), apt.New(), dep.OutputSink, dep.SummarySink, os.Stderr, appcontainer.SetupBuildahInput{
					ExtraPackages:   cmd.String("extra-packages"),
					InstallPackages: cmd.Bool("install-packages"),
					ProbeBuild:      cmd.Bool("probe-build"),
					PrintStore:      cmd.Bool("print-store"),
					WriteSummary:    cmd.Bool("summary"),
					StorageConf:     cmd.String("storage-conf"),
					StorageRoot:     cmd.String("storage-root"),
					TmpDir:          cmd.String("tmp-dir"),
					RunnerTemp:      cmd.String(flagTempDir),
					EnvFile:         cmd.String("env-file"),
				})

				return err
			})
		},
	}
}
