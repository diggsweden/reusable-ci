// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/gotool"
	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
	"github.com/diggsweden/reusable-ci/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/domain/build"
)

func goCmd() *cli.Command {
	return &cli.Command{
		Name:  "go",
		Usage: "Go build helpers",
		Commands: []*cli.Command{
			goMetadataCmd(),
			goDownloadCmd(),
			goTestCmd(),
			goSBOMCmd(),
			goCompileCmd(),
		},
	}
}

func goDownloadCmd() *cli.Command {
	return &cli.Command{
		Name:  "download",
		Usage: "download Go module dependencies",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagWorkingDir, Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "directory containing the go.mod file"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appbuild.GoDownload(ctx, gotool.Go{}, os.Stderr, os.Stderr, cmd.String(flagWorkingDir))
		},
	}
}

func goTestCmd() *cli.Command {
	return &cli.Command{
		Name:  "test",
		Usage: "run go test ./... with optional build tags",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagWorkingDir, Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "directory 'go test ./...' is run in"},
			&cli.StringFlag{Name: "build-tags", Sources: cli.EnvVars("BUILD_TAGS"), Usage: "comma-separated build tags passed via -tags"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appbuild.GoTest(ctx, gotool.Go{}, os.Stderr, os.Stderr, appbuild.GoTestInput{
				Dir:       cmd.String(flagWorkingDir),
				BuildTags: cmd.String("build-tags"),
			})
		},
	}
}

func goCompileCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdCompile,
		Usage: "cross-compile Go binaries into dist/",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagWorkingDir, Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "directory containing the go.mod file"},
			&cli.StringFlag{Name: flagBinaryName, Sources: cli.EnvVars("BINARY_NAME"), Usage: "explicit binary name (defaults to the go.mod module basename)"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "build-tags", Sources: cli.EnvVars("BUILD_TAGS"), Usage: "comma-separated build tags passed via -tags"},
			&cli.StringFlag{Name: "ldflags", Sources: cli.EnvVars("LD_FLAGS"), Usage: "extra -ldflags appended after the version-injection block"},
			&cli.StringFlag{Name: "main-package", Value: ".", Sources: cli.EnvVars("MAIN_PACKAGE"), Usage: "main package import path relative to --working-dir"},
			&cli.StringFlag{Name: "platforms", Value: build.DefaultPlatform, Sources: cli.EnvVars("PLATFORMS"), Usage: "comma-separated GOOS/GOARCH targets to cross-compile"},
			&cli.StringFlag{Name: flagVersion, Sources: cli.EnvVars("VERSION"), Usage: "release version baked into the binary via -ldflags (one of --version or --ref-name is required; use 'dev' for local builds)"},
			&cli.StringFlag{Name: flagRefName, Sources: cienv.RefName(), Usage: "git ref name used to derive the version when --version is empty (mirrors 'build go metadata')"}, //nolint:goconst // flag name reused across sibling subcommands.
			&cli.StringFlag{Name: "commit", Sources: cienv.Commit(), Usage: "commit SHA baked into the binary via -ldflags"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appbuild.GoBuildBinaries(ctx, gotool.Go{}, os.Stderr, os.Stderr, appbuild.GoBuildBinariesInput{
				Dir:         cmd.String(flagWorkingDir),
				BinaryName:  cmd.String(flagBinaryName),
				BuildTags:   cmd.String("build-tags"),
				LDFlags:     cmd.String("ldflags"),
				MainPackage: cmd.String("main-package"),
				Platforms:   cmd.String("platforms"),
				Version:     cmd.String(flagVersion),
				RefName:     cmd.String(flagRefName),
				Commit:      cmd.String("commit"),
			})
		},
	}
}

func goSBOMCmd() *cli.Command {
	return &cli.Command{
		Name:  "sbom",
		Usage: "generate a Go build SBOM with cyclonedx-gomod",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagWorkingDir, Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "directory containing the go.mod file"},
			&cli.StringFlag{Name: flagArtifactName, Sources: cli.EnvVars("ARTIFACT_NAME"), Usage: "explicit name override used in the SBOM filename"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: flagBinaryName, Sources: cli.EnvVars("BINARY_NAME"), Usage: "binary name component of the SBOM filename"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appbuild.GoBuildSBOM(ctx, gotool.CycloneDXGoMod{}, os.Stderr, os.Stderr, appbuild.GoBuildSBOMInput{
				Dir:          cmd.String(flagWorkingDir),
				ArtifactName: cmd.String(flagArtifactName),
				BinaryName:   cmd.String(flagBinaryName),
			})
		},
	}
}

func goMetadataCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdMetadata, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Usage: "read go.mod metadata and emit Go build outputs",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagWorkingDir, Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "directory containing the go.mod file"},
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
