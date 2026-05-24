// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/cargo"
	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
)

func cargoCmd() *cli.Command {
	return &cli.Command{
		Name:  "cargo",
		Usage: "Cargo build helpers (artefact-first Rust projects)",
		Commands: []*cli.Command{
			cargoMetadataCmd(),
			cargoFetchCmd(),
			cargoTestCmd(),
			cargoCompileCmd(),
		},
	}
}

func cargoFetchCmd() *cli.Command {
	return &cli.Command{
		Name:  "fetch",
		Usage: "cargo fetch --locked",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagWorkingDir, Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: usageCargoDirectory},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appbuild.CargoFetch(ctx, cargo.New(), os.Stderr, os.Stderr, cmd.String(flagWorkingDir))
		},
	}
}

func cargoTestCmd() *cli.Command {
	return &cli.Command{
		Name:  "test",
		Usage: "cargo test --locked --all-targets",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagWorkingDir, Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: usageCargoDirectory},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appbuild.CargoTest(ctx, cargo.New(), os.Stderr, os.Stderr, appbuild.CargoTestInput{Dir: cmd.String(flagWorkingDir)})
		},
	}
}

func cargoCompileCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdCompile,
		Usage: "cross-compile Rust binaries into dist/",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagWorkingDir, Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: usageCargoDirectory},
			&cli.StringFlag{Name: flagBinaryName, Sources: cli.EnvVars("BINARY_NAME"), Usage: "explicit binary name (defaults to the Cargo.toml [[bin]] target or package name)"},
			&cli.StringFlag{Name: "platforms", Value: "linux/amd64", Sources: cli.EnvVars("PLATFORMS"), Usage: "comma-separated GOOS/GOARCH targets to cross-compile (each must have a Rust target triple mapping)"},
			&cli.StringFlag{Name: flagVersion, Sources: cli.EnvVars("VERSION"), Usage: "release version (one of --version or --ref-name is required; use 'dev' for local builds)"},
			&cli.StringFlag{Name: flagRefName, Sources: cli.EnvVars("REF_NAME", "GITHUB_REF_NAME"), Usage: usageVersionRef},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appbuild.CargoBuildBinaries(ctx, cargo.New(), os.Stderr, os.Stderr, appbuild.CargoBuildBinariesInput{
				Dir:        cmd.String(flagWorkingDir),
				BinaryName: cmd.String(flagBinaryName),
				Platforms:  cmd.String("platforms"),
				Version:    cmd.String(flagVersion),
				RefName:    cmd.String(flagRefName),
			})
		},
	}
}

func cargoMetadataCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdMetadata,
		Usage: "read Cargo.toml metadata and emit Cargo build outputs",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagWorkingDir, Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: usageCargoDirectory},
			&cli.StringFlag{Name: flagArtifactName, Sources: cli.EnvVars("ARTIFACT_NAME"), Usage: "explicit artifact-name override"},
			&cli.StringFlag{Name: flagBinaryName, Sources: cli.EnvVars("BINARY_NAME"), Usage: "explicit binary-name override (skips the Cargo.toml [[bin]]/package heuristic)"},
			&cli.StringFlag{Name: flagVersion, Sources: cli.EnvVars("VERSION"), Usage: "explicit version override (skips the --ref-name and Cargo.toml fallbacks)"},
			&cli.StringFlag{Name: flagRefName, Sources: cli.EnvVars("REF_NAME", "GITHUB_REF_NAME"), Usage: usageVersionRef},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appbuild.CargoMetadata(ctx, cargo.New(), d.OutputSink, os.Stderr, appbuild.CargoMetadataInput{
					Dir:          cmd.String(flagWorkingDir),
					ArtifactName: cmd.String(flagArtifactName),
					BinaryName:   cmd.String(flagBinaryName),
					Version:      cmd.String(flagVersion),
					RefName:      cmd.String(flagRefName),
				})
			})
		},
	}
}
