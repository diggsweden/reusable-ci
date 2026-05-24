// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/gradle"
	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
)

func gradleAndroidCmd() *cli.Command {
	return &cli.Command{
		Name:  "gradle-android",
		Usage: "android (gradle) build pipeline",
		Commands: []*cli.Command{
			gradleAndroidArtifactNamesCmd(),
			gradleAndroidVersionInfoCmd(),
			gradleAndroidDecodeKeystoreCmd(),
			gradleAndroidWriteSecretsPropertiesCmd(),
			gradleAndroidResolveBuildTasksCmd(),
			gradleAndroidCompileCmd(),
			gradleAndroidListArtifactsCmd(),
		},
	}
}

func gradleAndroidWriteSecretsPropertiesCmd() *cli.Command {
	return &cli.Command{
		Name:  "write-secrets-properties",
		Usage: "base64-decode $SECRETS_PROPERTIES_BASE64 into secrets.properties (mode 0600); empty secret is a no-op",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "base64", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				Sources: cli.EnvVars("SECRETS_PROPERTIES_BASE64", "SECRETS_PROPERTIES"),
				Usage:   "base64-encoded secrets.properties body",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return appbuild.AndroidWriteSecretsProperties(os.Stderr, appbuild.AndroidWriteSecretsPropertiesInput{
				Base64: cmd.String("base64"),
			})
		},
	}
}

func gradleAndroidArtifactNamesCmd() *cli.Command {
	return &cli.Command{
		Name:  "artifact-names",
		Usage: "compute upload-artifact names (debug/release/aab/sbom)",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "include-date", Value: true, Sources: cli.EnvVars("INCLUDE_DATE_STAMP"), Usage: "append a YYYYMMDD-HHMMSS stamp to the artifact name"},
			&cli.StringFlag{Name: "prefix", Sources: cli.EnvVars("ARTIFACT_NAME_PREFIX"), Usage: "optional prefix prepended to every artifact name"},
			&cli.StringFlag{Name: "repo-name", Sources: cli.EnvVars("REPOSITORY_NAME"), Usage: "repository basename used in the default name"},
			&cli.StringFlag{Name: "flavor", Sources: cli.EnvVars("PRODUCT_FLAVOR"), Usage: "Android product flavor; included in the name when set"},
			&cli.StringFlag{Name: "override", Sources: cli.EnvVars("ARTIFACT_NAME"), Usage: "explicit name override; bypasses all heuristics"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appbuild.AndroidArtifactNames(ctx, d.OutputSink, os.Stderr, appbuild.AndroidArtifactNamesInput{
					IncludeDate: cmd.Bool("include-date"),
					Prefix:      cmd.String("prefix"),
					RepoName:    cmd.String("repo-name"),
					Flavor:      cmd.String("flavor"),
					Override:    cmd.String("override"),
				})
			})
		},
	}
}

func gradleAndroidVersionInfoCmd() *cli.Command {
	return &cli.Command{
		Name:  "version-info",
		Usage: "read versionName / versionCode from gradle.properties and emit outputs",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)

				return appbuild.AndroidVersionInfo(ctx, d.OutputSink, os.Stderr, annot, appbuild.AndroidVersionInfoInput{})
			})
		},
	}
}

func gradleAndroidDecodeKeystoreCmd() *cli.Command {
	return &cli.Command{
		Name:  "decode-keystore",
		Usage: "base64-decode $ANDROID_KEYSTORE_BASE64 to release.keystore; prints ANDROID_KEYSTORE_PATH=<path> to stdout (workflow redirects to $GITHUB_ENV)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "base64",
				Sources: cli.EnvVars("ANDROID_KEYSTORE_BASE64"),
				Usage:   "base64-encoded keystore body",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			// decode-keystore's primary output is `ANDROID_KEYSTORE_PATH=<path>`,
			// captured by `>> $GITHUB_ENV` in the workflow. The value goes to
			// stdout per clig.dev; progress/errors go to stderr.
			return appbuild.AndroidDecodeKeystore(os.Stdout, os.Stderr, appbuild.AndroidDecodeKeystoreInput{
				Base64: cmd.String("base64"),
			})
		},
	}
}

func gradleAndroidResolveBuildTasksCmd() *cli.Command {
	return &cli.Command{
		Name:  "resolve-build-tasks",
		Usage: "compute the gradle task list from flavor / build-types / include-aab / build-module",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "override", Sources: cli.EnvVars("GRADLE_TASKS_OVERRIDE"), Usage: "explicit task list override; bypasses the build-type heuristics"},
			&cli.StringFlag{Name: "flavor", Sources: cli.EnvVars("PRODUCT_FLAVOR"), Usage: "Android product flavor inserted into the task names"},
			&cli.StringFlag{Name: "build-types", Value: "debug,release", Sources: cli.EnvVars("BUILD_TYPES"), Usage: "comma-separated Android build types to assemble"},
			&cli.BoolFlag{Name: "include-aab", Value: true, Sources: cli.EnvVars("INCLUDE_AAB"), Usage: "also emit bundleRelease (produces an AAB)"},
			&cli.StringFlag{Name: "build-module", Value: "app", Sources: cli.EnvVars("BUILD_MODULE"), Usage: "gradle module name (e.g. \"app\")"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appbuild.AndroidResolveBuildTasks(ctx, d.OutputSink, os.Stderr, appbuild.AndroidResolveBuildTasksInput{
					Override:    cmd.String("override"),
					Flavor:      cmd.String("flavor"),
					BuildTypes:  cmd.String("build-types"),
					IncludeAAB:  cmd.Bool("include-aab"),
					BuildModule: cmd.String("build-module"),
				})
			})
		},
	}
}

func gradleAndroidCompileCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdCompile,
		Usage: "run ./gradlew with the resolved task list (and optional -x test)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "tasks", Sources: cli.EnvVars("GRADLE_TASKS"), Usage: "whitespace-separated gradle tasks to run"},
			&cli.BoolFlag{Name: "skip-tests", Sources: cli.EnvVars("SKIP_TESTS"), Usage: "append -x test to skip the test task"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appbuild.AndroidGradleBuild(ctx, gradle.New(), os.Stderr, os.Stderr, appbuild.AndroidGradleBuildInput{
				Tasks:     cmd.String("tasks"),
				SkipTests: cmd.Bool("skip-tests"),
			})
		},
	}
}

func gradleAndroidListArtifactsCmd() *cli.Command {
	return &cli.Command{
		Name:  "list-artifacts",
		Usage: "list APK/AAB files under <build-module>/build/outputs",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "build-module", Value: "app", Sources: cli.EnvVars("BUILD_MODULE"), Usage: "gradle module under which the APK/AAB outputs live"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return appbuild.AndroidListArtifacts(os.Stderr, appbuild.AndroidListArtifactsInput{
				BuildModule: cmd.String("build-module"),
			})
		},
	}
}
