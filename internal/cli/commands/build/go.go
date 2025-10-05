// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gotool"
	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/build"
)

func goCmd() *cli.Command {
	return &cli.Command{
		Name:  "go",
		Usage: "Go build helpers",
		Commands: []*cli.Command{
			goRunCmd(),
			goMetadataCmd(),
			goDownloadCmd(),
		},
	}
}

// goRunCmd runs the whole Go release build in one step. It is what a forge build
// job calls; the granular subcommands above remain the composable units it
// orchestrates. The job wraps this only with checkout + artifact upload.
func goRunCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdRun,
		Usage: "run the full Go release build (metadata, deps, test, SBOM, compile, summary)",
		Description: `Runs the whole Go build sequence in one step: resolve metadata, download
   dependencies, test (unless --skip-tests), generate the Build SBOM (unless
   --no-build-sbom), cross-compile per platform into dist/, and write the build
   summary. Binaries and the SBOM land at fixed paths the forge job uploads.

EXAMPLE:
   reusable-ci build go run --working-dir . --platforms linux/amd64,linux/arm64 --version 1.2.3`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagWorkingDir, Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: usageGoModDirectory},
			&cli.StringFlag{Name: flagArtifactName, Sources: cli.EnvVars("ARTIFACT_NAME"), Usage: "explicit artifact-name override (also names the SBOM)"},
			&cli.StringFlag{Name: flagBinaryName, Sources: cli.EnvVars("BINARY_NAME"), Usage: "explicit binary name (defaults to artifact-name, then go.mod module basename)"},
			&cli.StringFlag{Name: flagVersion, Sources: cli.EnvVars("VERSION"), Usage: "release version baked via -ldflags (defaults to --ref-name; 'dev' when neither set)"},
			&cli.StringFlag{Name: flagRefName, Sources: cienv.RefName(), Usage: usageVersionRef},
			&cli.StringFlag{Name: "commit", Sources: cienv.Commit(), Usage: "commit SHA baked into the binary via -ldflags"},
			&cli.StringFlag{Name: flagPlatforms, Value: build.DefaultPlatform, Sources: cli.EnvVars("PLATFORMS"), Usage: "comma/space/newline-separated GOOS/GOARCH targets to cross-compile"},
			&cli.StringFlag{Name: flagBuildTags, Sources: cli.EnvVars("BUILD_TAGS"), Usage: "comma-separated build tags passed via -tags (test + compile)"},
			&cli.StringFlag{Name: "ldflags", Sources: cli.EnvVars("LD_FLAGS"), Usage: "extra -ldflags appended after the version-injection block"},
			&cli.StringFlag{Name: "main-package", Value: ".", Sources: cli.EnvVars("MAIN_PACKAGE"), Usage: "main package import path relative to --working-dir"},
			&cli.BoolFlag{Name: flagSkipTests, Sources: cli.EnvVars("SKIP_TESTS"), Usage: "skip 'go test ./...' during the release build"},
			&cli.BoolFlag{Name: flagBuildSBOM, Value: true, Sources: cli.EnvVars("ENABLE_BUILD_SBOM"), Usage: "generate the CycloneDX Build SBOM (default true)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appbuild.GoReleaseBuild(ctx, d.SummarySink, gotool.Go{}, gotool.CycloneDXGoMod{}, os.Stderr, os.Stderr, appbuild.GoReleaseBuildInput{
					ReleaseBuildOptions: appbuild.ReleaseBuildOptions{
						Dir:             cmd.String(flagWorkingDir),
						ArtifactName:    cmd.String(flagArtifactName),
						SkipTests:       cmd.Bool(flagSkipTests),
						EnableBuildSBOM: cmd.Bool(flagBuildSBOM),
					},
					BinaryName:  cmd.String(flagBinaryName),
					Version:     cmd.String(flagVersion),
					RefName:     cmd.String(flagRefName),
					Commit:      cmd.String("commit"),
					Platforms:   cmd.String(flagPlatforms),
					BuildTags:   cmd.String(flagBuildTags),
					LDFlags:     cmd.String("ldflags"),
					MainPackage: cmd.String("main-package"),
				})
			})
		},
	}
}

func goDownloadCmd() *cli.Command {
	return &cli.Command{
		Name:  "download",
		Usage: "download Go module dependencies",
		Description: `EXAMPLE:
   reusable-ci build go download --working-dir .`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagWorkingDir, Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: usageGoModDirectory}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appbuild.GoDownload(ctx, gotool.Go{}, os.Stderr, os.Stderr, cmd.String(flagWorkingDir))
		},
	}
}

func goMetadataCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdMetadata, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Usage: "read go.mod metadata and emit Go build outputs",
		Description: `EXAMPLE:
   reusable-ci build go metadata --working-dir .`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagWorkingDir, Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: usageGoModDirectory},
			&cli.StringFlag{Name: flagArtifactName, Sources: cli.EnvVars("ARTIFACT_NAME"), Usage: "explicit artifact-name override (skips the module-basename heuristic)"},
			&cli.StringFlag{Name: flagBinaryName, Sources: cli.EnvVars("BINARY_NAME"), Usage: "explicit binary-name override (skips the module-basename heuristic)"},
			&cli.StringFlag{Name: flagVersion, Sources: cli.EnvVars("VERSION"), Usage: "explicit version override (skips the --ref-name heuristic)"},
			&cli.StringFlag{Name: flagRefName, Sources: cienv.RefName(), Usage: usageVersionRef},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appbuild.GoMetadata(ctx, d.OutputSink, os.Stderr, appbuild.GoMetadataInput{
					Dir:             cmd.String(flagWorkingDir),
					ArtifactName:    cmd.String(flagArtifactName),
					BinaryNameInput: cmd.String(flagBinaryName),
					VersionInput:    cmd.String(flagVersion),
					RefName:         cmd.String(flagRefName),
				})
			})
		},
	}
}
