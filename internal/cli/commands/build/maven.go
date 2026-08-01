// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/maven"
	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
)

func mavenCmd() *cli.Command {
	return &cli.Command{
		Name:  "maven",
		Usage: "maven build wrappers",
		Commands: []*cli.Command{
			mavenRunCmd(),
			mavenMetadataCmd(),
		},
	}
}

// mavenRunCmd runs the whole Maven release build in one step (the Maven sibling
// of `build go run`); the granular subcommands remain the composable units.
func mavenRunCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdRun,
		Usage: "run the full Maven release build (install, metadata, app/lib build, SBOM, summary)",
		Description: `Runs the whole Maven build sequence in one step: install modules to the
   local repo, resolve metadata, build the application (--build-type app) or
   library (--build-type lib), generate the Build SBOM with the pinned
   cyclonedx-maven-plugin (unless --no-build-sbom), and write the summaries.

   mvn executes in the current directory, not --working-dir: run this from the
   project root, or cd into the module first (the forge job does, then wraps it
   with checkout + artifact upload). --working-dir only locates the pom.xml for
   metadata.

EXAMPLE:
   cd module && reusable-ci build maven run --build-type app --cli-opts "-B -ntp" --sbom-tool-version 2.9.1`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagWorkingDir, Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "directory of the pom.xml to read for metadata; mvn itself runs in the current directory (cd in first)"},
			&cli.StringFlag{Name: "build-type", Sources: cli.EnvVars("BUILD_TYPE"), Usage: "build shape: \"app\" (clean package) or \"lib\" (sources + javadoc)"},
			&cli.StringFlag{Name: flagCLIOpts, Sources: cli.EnvVars("MAVEN_CLI_OPTS"), Usage: "extra args forwarded to mvn (whitespace-separated, e.g. \"-B -ntp\")"},
			&cli.StringFlag{Name: "profile", Sources: cli.EnvVars("MAVEN_PROFILE"), Usage: "Maven profile to activate (library builds)"},
			&cli.BoolFlag{Name: flagSkipTests, Sources: cli.EnvVars("SKIP_TESTS"), Usage: "skip the Maven test phase"},
			&cli.BoolFlag{Name: flagBuildSBOM, Value: true, Sources: cli.EnvVars("ENABLE_BUILD_SBOM"), Usage: "generate the cyclonedx-maven-plugin Build SBOM (default true)"},
			&cli.StringFlag{Name: flagSBOMToolVersion, Sources: cli.EnvVars("CYCLONEDX_MAVEN_VERSION"), Usage: "pinned org.cyclonedx:cyclonedx-maven-plugin version (required when --build-sbom)"},
			&cli.StringFlag{Name: "java-version", Sources: cli.EnvVars("JAVA_VERSION"), Usage: "JDK major version, reported in the build summary"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appbuild.MavenReleaseBuild(ctx, d.SummarySink, maven.New(), os.Stderr, os.Stderr, appbuild.MavenReleaseBuildInput{
					ReleaseBuildOptions: appbuild.ReleaseBuildOptions{
						Dir:             cmd.String(flagWorkingDir),
						SkipTests:       cmd.Bool(flagSkipTests),
						EnableBuildSBOM: cmd.Bool(flagBuildSBOM),
					},
					BuildType:       cmd.String("build-type"),
					CLIOpts:         strings.Fields(cmd.String(flagCLIOpts)),
					Profile:         cmd.String("profile"),
					SBOMToolVersion: cmd.String(flagSBOMToolVersion),
					JavaVersion:     cmd.String("java-version"),
				})
			})
		},
	}
}

func mavenMetadataCmd() *cli.Command {
	return &cli.Command{
		Name:  "metadata", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Usage: "parse pom.xml for {version,groupId,artifactId} and emit CI outputs (falls back to `mvn help:evaluate` only for ${property} references)",
		Description: `EXAMPLE:
   reusable-ci build maven metadata --working-dir .`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "working-dir", Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "directory containing the pom.xml to parse"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appbuild.MavenMetadata(ctx, d.OutputSink, maven.New(), os.Stderr, appbuild.MavenMetadataInput{
					Dir: cmd.String("working-dir"),
				})
			})
		},
	}
}
