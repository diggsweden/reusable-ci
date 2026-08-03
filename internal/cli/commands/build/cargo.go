// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cargo"
	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/build"
)

func cargoCmd() *cli.Command {
	return &cli.Command{
		Name:  "cargo",
		Usage: "Cargo build helpers (artifact-first Rust projects)",
		Commands: []*cli.Command{
			cargoRunCmd(),
			cargoMetadataCmd(),
		},
	}
}

// cargoRunCmd runs the whole Cargo release build in one step (the Cargo sibling
// of `build go run`); the granular subcommands remain the composable units.
func cargoRunCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdRun,
		Usage: "run the full Cargo release build (metadata, fetch, test, SBOM, compile, status)",
		Description: `Runs the whole Cargo build sequence in one step: resolve metadata, fetch
   deps, test (unless --skip-tests), generate the Build SBOM with cargo-cyclonedx
   (unless --no-build-sbom), and cross-compile per platform into dist/. The
   forge job wraps this with checkout + artifact upload only.

EXAMPLE:
   reusable-ci build cargo run --working-dir . --platforms linux/amd64,linux/arm64 --version 1.2.3`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagWorkingDir, Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: usageCargoDirectory},
			&cli.StringFlag{Name: flagArtifactName, Sources: cli.EnvVars("ARTIFACT_NAME"), Usage: "explicit artifact-name override (also names the SBOM)"},
			&cli.StringFlag{Name: flagBinaryName, Sources: cli.EnvVars("BINARY_NAME"), Usage: "explicit binary name (defaults to the Cargo.toml [[bin]] target or package name)"},
			&cli.StringFlag{Name: flagVersion, Sources: cli.EnvVars("VERSION"), Usage: "release version (defaults to --ref-name, then Cargo.toml; 'dev' when none)"},
			&cli.StringFlag{Name: flagRefName, Sources: cienv.RefName(), Usage: usageVersionRef},
			&cli.StringFlag{Name: flagPlatforms, Value: build.DefaultPlatform, Sources: cli.EnvVars("PLATFORMS"), Usage: "comma/space/newline-separated GOOS/GOARCH targets to cross-compile (each must have a Rust target triple mapping)"},
			&cli.BoolFlag{Name: flagSkipTests, Sources: cli.EnvVars("SKIP_TESTS"), Usage: "skip 'cargo test' during the release build"},
			&cli.BoolFlag{Name: flagBuildSBOM, Value: true, Sources: cli.EnvVars("ENABLE_BUILD_SBOM"), Usage: "generate the cargo-cyclonedx Build SBOM (default true)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appbuild.CargoReleaseBuild(ctx, d.SummarySink, cargo.New(), os.Stderr, os.Stderr, appbuild.CargoReleaseBuildInput{
					ReleaseBuildOptions: appbuild.ReleaseBuildOptions{
						Dir:             cmd.String(flagWorkingDir),
						ArtifactName:    cmd.String(flagArtifactName),
						SkipTests:       cmd.Bool(flagSkipTests),
						EnableBuildSBOM: cmd.Bool(flagBuildSBOM),
					},
					BinaryName: cmd.String(flagBinaryName),
					Version:    cmd.String(flagVersion),
					RefName:    cmd.String(flagRefName),
					Platforms:  cmd.String(flagPlatforms),
				})
			})
		},
	}
}

func cargoMetadataCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdMetadata,
		Usage: "read Cargo.toml metadata and emit Cargo build outputs",
		Description: `EXAMPLE:
   reusable-ci build cargo metadata --working-dir .`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagWorkingDir, Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: usageCargoDirectory},
			&cli.StringFlag{Name: flagArtifactName, Sources: cli.EnvVars("ARTIFACT_NAME"), Usage: "explicit artifact-name override"},
			&cli.StringFlag{Name: flagBinaryName, Sources: cli.EnvVars("BINARY_NAME"), Usage: "explicit binary-name override (skips the Cargo.toml [[bin]]/package heuristic)"},
			&cli.StringFlag{Name: flagVersion, Sources: cli.EnvVars("VERSION"), Usage: "explicit version override (skips the --ref-name and Cargo.toml fallbacks)"},
			&cli.StringFlag{Name: flagRefName, Sources: cienv.RefName(), Usage: usageVersionRef},
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
