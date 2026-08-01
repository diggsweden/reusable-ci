// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/npm"
	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
)

func npmCmd() *cli.Command {
	return &cli.Command{
		Name:  "npm",
		Usage: "NPM build helpers",
		Commands: []*cli.Command{
			npmRunCmd(),
			npmMetadataCmd(),
			npmPackCmd(),
		},
	}
}

// npmRunCmd runs the whole npm release build in one step (the npm sibling of
// `build go run`); the granular subcommands remain the composable units.
func npmRunCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdRun,
		Usage: "run the full npm release build (metadata, ci, test, build, SBOM, pack, summary)",
		Description: `Runs the whole npm build sequence in one step: resolve metadata, npm ci,
   test (soft, unless --skip-tests), run the build script, generate the Build
   SBOM with the pinned cyclonedx-npm (unless --no-build-sbom), and npm pack. The
   tarball is left at its default <name>-<version>.tgz path for the forge job to
   upload.

EXAMPLE:
   reusable-ci build npm run --working-dir . --sbom-tool-version 4.2.1`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagWorkingDir, Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: usageNPMDirectory},
			&cli.StringFlag{Name: "scope", Sources: cli.EnvVars("SCOPE", "PACKAGE_SCOPE"), Usage: "expected scope (e.g. @examplescope); errors when package.json disagrees"},
			&cli.StringFlag{Name: "script", Value: subCmdBuild, Usage: "npm build script name to run (skipped when absent)"},
			&cli.BoolFlag{Name: flagSkipTests, Sources: cli.EnvVars("SKIP_TESTS"), Usage: "skip the (soft) npm test run"},
			&cli.BoolFlag{Name: flagBuildSBOM, Value: true, Sources: cli.EnvVars("ENABLE_BUILD_SBOM"), Usage: "generate the CycloneDX Build SBOM (default true)"},
			&cli.StringFlag{Name: flagSBOMToolVersion, Sources: cli.EnvVars("CYCLONEDX_VERSION"), Usage: "pinned @cyclonedx/cyclonedx-npm version run via npx (required when --build-sbom)"},
			&cli.StringFlag{Name: "node-version", Sources: cli.EnvVars("NODE_VERSION"), Usage: "Node major version, reported in the build summary"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appbuild.NPMReleaseBuild(ctx, d.SummarySink, npm.New(), npm.NewNpx(), deps.Annotator(cmd), os.Stderr, os.Stderr, appbuild.NPMReleaseBuildInput{
					ReleaseBuildOptions: appbuild.ReleaseBuildOptions{
						Dir:             cmd.String(flagWorkingDir),
						SkipTests:       cmd.Bool(flagSkipTests),
						EnableBuildSBOM: cmd.Bool(flagBuildSBOM),
					},
					PackageScope:    cmd.String("scope"),
					ScriptName:      cmd.String("script"),
					SBOMToolVersion: cmd.String(flagSBOMToolVersion),
					NodeVersion:     cmd.String("node-version"),
				})
			})
		},
	}
}

func npmMetadataCmd() *cli.Command {
	return &cli.Command{
		Name:  "metadata", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Usage: "read package.json metadata and emit CI outputs",
		Description: `EXAMPLE:
   reusable-ci build npm metadata --scope @org --working-dir .`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagWorkingDir, Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: usageNPMDirectory},
			&cli.StringFlag{Name: "scope", Sources: cli.EnvVars("SCOPE", "PACKAGE_SCOPE"), Usage: "expected scope (e.g. @examplescope); errors when package.json disagrees"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)

				return appbuild.NPMMetadata(ctx, d.OutputSink, os.Stderr, annot, appbuild.NPMMetadataInput{
					Dir:          cmd.String(flagWorkingDir),
					PackageScope: cmd.String("scope"),
				})
			})
		},
	}
}

func npmPackCmd() *cli.Command {
	return &cli.Command{
		Name:  "pack",
		Usage: "run npm pack --json and emit the tarball output",
		Description: `EXAMPLE:
   reusable-ci build npm pack --working-dir .`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagWorkingDir, Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "directory containing the package.json 'npm pack' runs against"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appbuild.NPMPack(ctx, npm.New(), d.OutputSink, os.Stderr, os.Stderr, appbuild.NPMMetadataInput{Dir: cmd.String(flagWorkingDir)})
			})
		},
	}
}
