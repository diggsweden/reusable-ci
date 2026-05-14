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
			gradleAndroidResolveBuildTasksCmd(),
			gradleAndroidBuildCmd(),
			gradleAndroidListArtifactsCmd(),
		},
	}
}

func gradleAndroidArtifactNamesCmd() *cli.Command {
	return &cli.Command{
		Name:  "artifact-names",
		Usage: "compute upload-artifact names (debug/release/aab/sbom)",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "include-date", Value: true, Sources: cli.EnvVars("INCLUDE_DATE_STAMP")},
			&cli.StringFlag{Name: "prefix", Sources: cli.EnvVars("ARTIFACT_NAME_PREFIX")},
			&cli.StringFlag{Name: "repo-name", Sources: cli.EnvVars("REPOSITORY_NAME")},
			&cli.StringFlag{Name: "flavor", Sources: cli.EnvVars("PRODUCT_FLAVOR")},
			&cli.StringFlag{Name: "override", Sources: cli.EnvVars("ARTIFACT_NAME")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			return appbuild.AndroidArtifactNames(ctx, d.OutputSink, os.Stderr, appbuild.AndroidArtifactNamesInput{
				IncludeDate: cmd.Bool("include-date"),
				Prefix:      cmd.String("prefix"),
				RepoName:    cmd.String("repo-name"),
				Flavor:      cmd.String("flavor"),
				Override:    cmd.String("override"),
			})
		},
	}
}

func gradleAndroidVersionInfoCmd() *cli.Command {
	return &cli.Command{
		Name:  "version-info",
		Usage: "read versionName / versionCode from gradle.properties and emit outputs",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			annot := deps.Annotator(cmd)
			return appbuild.AndroidVersionInfo(ctx, d.OutputSink, os.Stderr, annot, appbuild.AndroidVersionInfoInput{})
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
			&cli.StringFlag{Name: "flavor", Sources: cli.EnvVars("PRODUCT_FLAVOR")},
			&cli.StringFlag{Name: "build-types", Value: "debug,release", Sources: cli.EnvVars("BUILD_TYPES")},
			&cli.BoolFlag{Name: "include-aab", Value: true, Sources: cli.EnvVars("INCLUDE_AAB")},
			&cli.StringFlag{Name: "build-module", Value: "app", Sources: cli.EnvVars("BUILD_MODULE")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			return appbuild.AndroidResolveBuildTasks(ctx, d.OutputSink, os.Stderr, appbuild.AndroidResolveBuildTasksInput{
				Flavor:      cmd.String("flavor"),
				BuildTypes:  cmd.String("build-types"),
				IncludeAAB:  cmd.Bool("include-aab"),
				BuildModule: cmd.String("build-module"),
			})
		},
	}
}

func gradleAndroidBuildCmd() *cli.Command {
	return &cli.Command{
		Name:  "build",
		Usage: "run ./gradlew with the resolved task list (and optional -x test)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "tasks", Sources: cli.EnvVars("GRADLE_TASKS")},
			&cli.BoolFlag{Name: "skip-tests", Sources: cli.EnvVars("SKIP_TESTS")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appbuild.AndroidGradleBuild(ctx, gradle.New(), os.Stdout, os.Stderr, appbuild.AndroidGradleBuildInput{
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
			&cli.StringFlag{Name: "build-module", Value: "app", Sources: cli.EnvVars("BUILD_MODULE")},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return appbuild.AndroidListArtifacts(os.Stdout, appbuild.AndroidListArtifactsInput{
				BuildModule: cmd.String("build-module"),
			})
		},
	}
}
