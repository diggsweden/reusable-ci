// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gradle"
	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
)

func gradleCmd() *cli.Command {
	return &cli.Command{
		Name:  "gradle",
		Usage: "gradle (JVM) build wrappers",
		Commands: []*cli.Command{
			gradleRunCmd(),
			gradleMetadataCmd(),
		},
	}
}

// gradleRunCmd runs the whole Gradle release build in one step (the Gradle
// sibling of `build go run`); the granular subcommands remain the composable units.
func gradleRunCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdRun,
		Usage: "run the full Gradle release build (gradlew chmod, metadata, tasks, SBOM, summary)",
		Description: `Runs the whole Gradle build sequence in one step: make ./gradlew
   executable, resolve metadata, run the gradle tasks (-x test unless
   --skip-tests), generate the Build SBOM with the pinned cyclonedx-gradle-plugin
   (unless --no-build-sbom), and write the summaries.

   ./gradlew executes in the current directory, not --working-dir: run this from
   the project root, or cd in first (the forge job does, then wraps it with
   checkout + artifact upload). --working-dir only locates gradle.properties for
   metadata.

EXAMPLE:
   cd app && reusable-ci build gradle run --tasks assemble --sbom-tool-version 3.2.1`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagWorkingDir, Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "directory of gradle.properties to read for metadata; ./gradlew itself runs in the current directory (cd in first)"},
			&cli.StringFlag{Name: flagTasks, Required: true, Sources: cli.EnvVars("GRADLE_TASKS"), Usage: usageGradleTasks},
			&cli.BoolFlag{Name: flagSkipTests, Sources: cli.EnvVars("SKIP_TESTS"), Usage: usageSkipTestTask},
			&cli.BoolFlag{Name: flagBuildSBOM, Value: true, Sources: cli.EnvVars("ENABLE_BUILD_SBOM"), Usage: "generate the cyclonedx-gradle-plugin Build SBOM (default true)"},
			&cli.StringFlag{Name: flagSBOMToolVersion, Sources: cli.EnvVars("CYCLONEDX_GRADLE_VERSION"), Usage: "pinned cyclonedx-gradle-plugin version (required when --build-sbom)"},
			&cli.StringFlag{Name: "java-version", Sources: cli.EnvVars("JAVA_VERSION"), Usage: "JDK major version, reported in the build summary"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appbuild.GradleReleaseBuild(ctx, d.SummarySink, gradle.New(), os.Stderr, os.Stderr, appbuild.GradleReleaseBuildInput{
					ReleaseBuildOptions: appbuild.ReleaseBuildOptions{
						Dir:             cmd.String(flagWorkingDir),
						SkipTests:       cmd.Bool(flagSkipTests),
						EnableBuildSBOM: cmd.Bool(flagBuildSBOM),
					},
					Tasks:           cmd.String(flagTasks),
					SBOMToolVersion: cmd.String(flagSBOMToolVersion),
					JavaVersion:     cmd.String("java-version"),
				})
			})
		},
	}
}

func gradleMetadataCmd() *cli.Command {
	return &cli.Command{
		Name:  "metadata", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Usage: "read gradle.properties metadata and emit CI outputs",
		Description: `EXAMPLE:
   reusable-ci build gradle metadata --working-dir .`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "working-dir", Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "directory containing gradle.properties / build.gradle"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)

				return appbuild.GradleMetadata(ctx, d.OutputSink, os.Stderr, annot, appbuild.GradleMetadataInput{Dir: cmd.String("working-dir")})
			})
		},
	}
}
