// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package report

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// buildGroup wires `reusable-ci report build <ecosystem>` — every
// subcommand here appends a step-summary block describing one
// ecosystem-specific build (maven, npm, gradle, android, go, xcode).
func buildGroup() *cli.Command {
	return &cli.Command{
		Name:  "build",
		Usage: "append a per-ecosystem build summary to the step summary",
		Commands: []*cli.Command{
			buildMavenCmd(),
			buildNPMCmd(),
			buildGradleCmd(),
			buildAndroidCmd(),
			buildGoCmd(),
			buildXcodeCmd(),
		},
	}
}

func buildMavenCmd() *cli.Command {
	return &cli.Command{
		Name:  "maven",
		Usage: "append the Maven build summary block to the step summary",
		Description: `EXAMPLE:
   reusable-ci report build maven --group-id com.example --artifact-id app --version 1.2.3 --java-version 21`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "build-type", Sources: cli.EnvVars("BUILD_TYPE"), Usage: "Maven build type (library/application)"},
			&cli.StringFlag{Name: "group-id", Sources: cli.EnvVars("GROUP_ID"), Usage: "Maven groupId of the built artifact"},
			&cli.StringFlag{Name: "artifact-id", Sources: cli.EnvVars("ARTIFACT_ID"), Usage: "Maven artifactId of the built artifact"},
			&cli.StringFlag{Name: "version", Sources: cli.EnvVars("VERSION"), Usage: "Maven version of the built artifact"},                          //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "java-version", Sources: cli.EnvVars("JAVA_VERSION"), Usage: "JDK major version used for the build"},               //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.BoolFlag{Name: "skip-tests", Sources: cli.EnvVars("SKIP_TESTS"), Usage: "tests were skipped (toggles the test-status summary row)"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.BoolFlag{Name: "is-snapshot", Sources: cli.EnvVars("IS_SNAPSHOT"), Usage: "the built version is a -SNAPSHOT"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			for _, f := range []string{"build-type", "group-id", "artifact-id", "version", "java-version"} {
				if cmd.String(f) == "" {
					return fmt.Errorf("--%s is required: %w", f, errs.ErrUsage)
				}
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appsummary.MavenBuild(ctx, d.SummarySink, appsummary.MavenBuildInput{
					BuildType:   cmd.String("build-type"),
					GroupID:     cmd.String("group-id"),
					ArtifactID:  cmd.String("artifact-id"),
					Version:     cmd.String("version"),
					JavaVersion: cmd.String("java-version"),
					SkipTests:   cmd.Bool("skip-tests"),
					IsSnapshot:  cmd.Bool("is-snapshot"),
				})
			})
		},
	}
}

func buildNPMCmd() *cli.Command {
	return &cli.Command{
		Name:  "npm",
		Usage: "append the NPM build summary block to the step summary",
		Description: `EXAMPLE:
   reusable-ci report build npm --package-name @org/app --version 1.2.3 --node-version 22`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "package-name", Sources: cli.EnvVars("PACKAGE_NAME"), Usage: "npm package name from package.json"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "version", Sources: cli.EnvVars("VERSION"), Usage: "npm package version from package.json"},
			&cli.StringFlag{Name: "node-version", Sources: cli.EnvVars("NODE_VERSION"), Usage: "Node.js version used for the build"},
			&cli.BoolFlag{Name: "skip-tests", Sources: cli.EnvVars("SKIP_TESTS"), Usage: "tests were skipped (toggles the test-status summary row)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			for _, f := range []string{"package-name", "version", "node-version"} {
				if cmd.String(f) == "" {
					return fmt.Errorf("--%s is required: %w", f, errs.ErrUsage)
				}
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appsummary.NPMBuild(ctx, d.SummarySink, appsummary.NPMBuildInput{
					PackageName: cmd.String("package-name"),
					Version:     cmd.String("version"),
					NodeVersion: cmd.String("node-version"),
					SkipTests:   cmd.Bool("skip-tests"),
				})
			})
		},
	}
}

func buildGradleCmd() *cli.Command {
	return &cli.Command{
		Name:  "gradle",
		Usage: "append the Gradle (JVM) build summary block to the step summary",
		Description: `EXAMPLE:
   reusable-ci report build gradle --version 1.2.3 --java-version 21 --tasks "build"`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "java-version", Sources: cli.EnvVars("JAVA_VERSION"), Usage: "JDK major version used for the build"},
			&cli.StringFlag{Name: "tasks", Sources: cli.EnvVars("GRADLE_TASKS"), Usage: "gradle tasks that ran (shown verbatim in the summary)"},
			&cli.BoolFlag{Name: "skip-tests", Sources: cli.EnvVars("SKIP_TESTS"), Usage: "tests were skipped (toggles the test-status summary row)"},
			&cli.StringFlag{Name: "version", Sources: cli.EnvVars("VERSION"), Usage: "gradle project version (from gradle.properties)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			for _, f := range []string{"java-version", "tasks"} {
				if cmd.String(f) == "" {
					return fmt.Errorf("--%s is required: %w", f, errs.ErrUsage)
				}
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appsummary.GradleBuild(ctx, d.SummarySink, appsummary.GradleBuildInput{
					JavaVersion: cmd.String("java-version"),
					GradleTasks: cmd.String("tasks"),
					SkipTests:   cmd.Bool("skip-tests"),
					Version:     cmd.String("version"),
				})
			})
		},
	}
}

func buildAndroidCmd() *cli.Command {
	return &cli.Command{
		Name:  "android",
		Usage: "append the Android variants build summary block to the step summary",
		Description: `EXAMPLE:
   reusable-ci report build android --version 1.2.3 --version-code 42 --java-version 21`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "java-version", Sources: cli.EnvVars("JAVA_VERSION"), Usage: "JDK major version used for the build"},
			&cli.StringFlag{Name: "jdk-dist", Sources: cli.EnvVars("JDK_DIST"), Usage: "JDK distribution (e.g. temurin, zulu, corretto)"},
			&cli.StringFlag{Name: "build-module", Sources: cli.EnvVars("BUILD_MODULE"), Usage: "gradle module name (e.g. \"app\")"},
			&cli.StringFlag{Name: "flavor", Sources: cli.EnvVars("FLAVOR"), Usage: "Android product flavor"},
			&cli.StringFlag{Name: "build-types", Value: "debug,release", Sources: cli.EnvVars("BUILD_TYPES"), Usage: "comma-separated Android build types"},
			&cli.BoolFlag{Name: "include-aab", Value: true, Sources: cli.EnvVars("INCLUDE_AAB"), Usage: "an AAB was bundled (toggles its summary row)"},
			&cli.BoolFlag{Name: "signing", Sources: cli.EnvVars("SIGNING"), Usage: "release signing keys were applied (toggles the signing row)"},
			&cli.BoolFlag{Name: "skip-tests", Sources: cli.EnvVars("SKIP_TESTS"), Usage: "tests were skipped (toggles the test-status summary row)"},
			&cli.StringFlag{Name: "version", Sources: cli.EnvVars("VERSION"), Usage: "Android versionName from build.gradle"},
			&cli.StringFlag{Name: "version-code", Sources: cli.EnvVars("VERSION_CODE"), Usage: "Android versionCode from build.gradle"},
			&cli.StringFlag{Name: "debug-name", Sources: cli.EnvVars("DEBUG_NAME"), Usage: "filename of the debug APK"},
			&cli.StringFlag{Name: "release-name", Sources: cli.EnvVars("RELEASE_NAME"), Usage: "filename of the release APK"},
			&cli.StringFlag{Name: "aab-name", Sources: cli.EnvVars("AAB_NAME"), Usage: "filename of the bundled AAB"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			for _, f := range []string{"java-version", "jdk-dist", "build-module"} {
				if cmd.String(f) == "" {
					return fmt.Errorf("--%s is required: %w", f, errs.ErrUsage)
				}
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appsummary.AndroidBuild(ctx, d.SummarySink, appsummary.AndroidBuildInput{
					JavaVersion: cmd.String("java-version"),
					JDKDist:     cmd.String("jdk-dist"),
					BuildModule: cmd.String("build-module"),
					Flavor:      cmd.String("flavor"),
					BuildTypes:  cmd.String("build-types"),
					IncludeAAB:  cmd.Bool("include-aab"),
					Signing:     cmd.Bool("signing"),
					SkipTests:   cmd.Bool("skip-tests"),
					Version:     cmd.String("version"),
					VersionCode: cmd.String("version-code"),
					DebugName:   cmd.String("debug-name"),
					ReleaseName: cmd.String("release-name"),
					AABName:     cmd.String("aab-name"),
				})
			})
		},
	}
}

func buildGoCmd() *cli.Command {
	return &cli.Command{
		Name:  "go",
		Usage: "append the Go build summary block to the step summary",
		Description: `EXAMPLE:
   reusable-ci report build go --binary-name app --version 1.2.3 --platforms linux/amd64,linux/arm64`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "binary-name", Sources: cli.EnvVars("BINARY_NAME"), Usage: "name of the produced Go binary"},
			&cli.StringFlag{Name: "module", Sources: cli.EnvVars("MODULE"), Usage: "Go module path (from go.mod)"},
			&cli.StringFlag{Name: "platforms", Sources: cli.EnvVars("PLATFORMS"), Usage: "comma-separated GOOS/GOARCH pairs the binary was built for"},
			&cli.StringFlag{Name: "version", Sources: cli.EnvVars("VERSION"), Usage: "release version embedded via -ldflags"},
			&cli.BoolFlag{Name: "skip-tests", Sources: cli.EnvVars("SKIP_TESTS"), Usage: "tests were skipped (toggles the test-status summary row)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			for _, f := range []string{"binary-name", "module", "platforms", "version"} {
				if cmd.String(f) == "" {
					return fmt.Errorf("--%s is required: %w", f, errs.ErrUsage)
				}
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appsummary.GoBuild(ctx, d.SummarySink, appsummary.GoBuildInput{
					BinaryName: cmd.String("binary-name"),
					Module:     cmd.String("module"),
					Platforms:  cmd.String("platforms"),
					Version:    cmd.String("version"),
					SkipTests:  cmd.Bool("skip-tests"),
				})
			})
		},
	}
}

func buildXcodeCmd() *cli.Command {
	return &cli.Command{
		Name:  "xcode",
		Usage: "append the Xcode build summary block to the step summary",
		Description: `EXAMPLE:
   reusable-ci report build xcode --scheme App --version 1.2.3 --configuration Release`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "xcode-version", Sources: cli.EnvVars("XCODE_VERSION"), Usage: "Xcode version used for the build"},
			&cli.StringFlag{Name: "scheme", Sources: cli.EnvVars("SCHEME"), Usage: "Xcode scheme that was archived"},
			&cli.StringFlag{Name: "configuration", Value: "Release", Sources: cli.EnvVars("CONFIGURATION"), Usage: "Xcode build configuration"},
			&cli.StringFlag{Name: "destination", Value: "generic/platform=iOS", Sources: cli.EnvVars("DESTINATION"), Usage: "Xcode destination spec used for the archive"},
			&cli.BoolFlag{Name: "signing", Value: true, Sources: cli.EnvVars("SIGNING"), Usage: "code signing was performed (toggles the signing row)"},
			&cli.StringFlag{Name: "version", Value: "unknown", Sources: cli.EnvVars("VERSION"), Usage: "MARKETING_VERSION baked into the IPA"},
			&cli.StringFlag{Name: "build-number", Value: "unknown", Sources: cli.EnvVars("BUILD_NUMBER"), Usage: "CURRENT_PROJECT_VERSION baked into the IPA"},
			&cli.StringFlag{Name: "ipa-name", Sources: cli.EnvVars("IPA_NAME"), Usage: "filename of the produced IPA"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			for _, f := range []string{"xcode-version", "scheme"} {
				if cmd.String(f) == "" {
					return fmt.Errorf("--%s is required: %w", f, errs.ErrUsage)
				}
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appsummary.XcodeBuild(ctx, d.SummarySink, appsummary.XcodeBuildInput{
					XcodeVersion:  cmd.String("xcode-version"),
					Scheme:        cmd.String("scheme"),
					Configuration: cmd.String("configuration"),
					Destination:   cmd.String("destination"),
					Signing:       cmd.Bool("signing"),
					Version:       cmd.String("version"),
					BuildNumber:   cmd.String("build-number"),
					IPAName:       cmd.String("ipa-name"),
				})
			})
		},
	}
}
