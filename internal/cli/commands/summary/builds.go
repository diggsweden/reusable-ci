// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

func xcodeBuildCmd() *cli.Command {
	return &cli.Command{
		Name:  "xcode-build",
		Usage: "append the Xcode build summary block to the step summary",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "xcode-version", Sources: cli.EnvVars("XCODE_VERSION")},
			&cli.StringFlag{Name: "scheme", Sources: cli.EnvVars("SCHEME")},
			&cli.StringFlag{Name: "configuration", Value: "Release", Sources: cli.EnvVars("CONFIGURATION")},
			&cli.StringFlag{Name: "destination", Value: "generic/platform=iOS", Sources: cli.EnvVars("DESTINATION")},
			&cli.BoolFlag{Name: "signing", Value: true, Sources: cli.EnvVars("SIGNING")},
			&cli.StringFlag{Name: "version", Value: "unknown", Sources: cli.EnvVars("VERSION")},
			&cli.StringFlag{Name: "build-number", Value: "unknown", Sources: cli.EnvVars("BUILD_NUMBER")},
			&cli.StringFlag{Name: "ipa-name", Sources: cli.EnvVars("IPA_NAME")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			for _, f := range []string{"xcode-version", "scheme"} {
				if cmd.String(f) == "" {
					return fmt.Errorf("--%s is required: %w", f, errs.ErrUsage)
				}
			}
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
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
		},
	}
}

func androidBuildCmd() *cli.Command {
	return &cli.Command{
		Name:  "android-build",
		Usage: "append the Android variants build summary block to the step summary",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "java-version", Sources: cli.EnvVars("JAVA_VERSION")},
			&cli.StringFlag{Name: "jdk-dist", Sources: cli.EnvVars("JDK_DIST")},
			&cli.StringFlag{Name: "build-module", Sources: cli.EnvVars("BUILD_MODULE")},
			&cli.StringFlag{Name: "flavor", Sources: cli.EnvVars("FLAVOR")},
			&cli.StringFlag{Name: "build-types", Value: "debug,release", Sources: cli.EnvVars("BUILD_TYPES")},
			&cli.BoolFlag{Name: "include-aab", Value: true, Sources: cli.EnvVars("INCLUDE_AAB")},
			&cli.BoolFlag{Name: "signing", Sources: cli.EnvVars("SIGNING")},
			&cli.BoolFlag{Name: "skip-tests", Sources: cli.EnvVars("SKIP_TESTS")},
			&cli.StringFlag{Name: "version", Sources: cli.EnvVars("VERSION")},
			&cli.StringFlag{Name: "version-code", Sources: cli.EnvVars("VERSION_CODE")},
			&cli.StringFlag{Name: "debug-name", Sources: cli.EnvVars("DEBUG_NAME")},
			&cli.StringFlag{Name: "release-name", Sources: cli.EnvVars("RELEASE_NAME")},
			&cli.StringFlag{Name: "aab-name", Sources: cli.EnvVars("AAB_NAME")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			for _, f := range []string{"java-version", "jdk-dist", "build-module"} {
				if cmd.String(f) == "" {
					return fmt.Errorf("--%s is required: %w", f, errs.ErrUsage)
				}
			}
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
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
		},
	}
}

func gradleBuildCmd() *cli.Command {
	return &cli.Command{
		Name:  "gradle-build",
		Usage: "append the Gradle (JVM) build summary block to the step summary",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "java-version", Sources: cli.EnvVars("JAVA_VERSION")},
			&cli.StringFlag{Name: "gradle-tasks", Sources: cli.EnvVars("GRADLE_TASKS")},
			&cli.BoolFlag{Name: "skip-tests", Sources: cli.EnvVars("SKIP_TESTS")},
			&cli.StringFlag{Name: "version", Sources: cli.EnvVars("VERSION")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			for _, f := range []string{"java-version", "gradle-tasks"} {
				if cmd.String(f) == "" {
					return fmt.Errorf("--%s is required: %w", f, errs.ErrUsage)
				}
			}
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			return appsummary.GradleBuild(ctx, d.SummarySink, appsummary.GradleBuildInput{
				JavaVersion: cmd.String("java-version"),
				GradleTasks: cmd.String("gradle-tasks"),
				SkipTests:   cmd.Bool("skip-tests"),
				Version:     cmd.String("version"),
			})
		},
	}
}

func npmBuildCmd() *cli.Command {
	return &cli.Command{
		Name:  "npm-build",
		Usage: "append the NPM build summary block to the step summary",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "package-name", Sources: cli.EnvVars("PACKAGE_NAME")},
			&cli.StringFlag{Name: "version", Sources: cli.EnvVars("VERSION")},
			&cli.StringFlag{Name: "node-version", Sources: cli.EnvVars("NODE_VERSION")},
			&cli.BoolFlag{Name: "skip-tests", Sources: cli.EnvVars("SKIP_TESTS")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			for _, f := range []string{"package-name", "version", "node-version"} {
				if cmd.String(f) == "" {
					return fmt.Errorf("--%s is required: %w", f, errs.ErrUsage)
				}
			}
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			return appsummary.NPMBuild(ctx, d.SummarySink, appsummary.NPMBuildInput{
				PackageName: cmd.String("package-name"),
				Version:     cmd.String("version"),
				NodeVersion: cmd.String("node-version"),
				SkipTests:   cmd.Bool("skip-tests"),
			})
		},
	}
}

func mavenBuildCmd() *cli.Command {
	return &cli.Command{
		Name:  "maven-build",
		Usage: "append the Maven build summary block to the step summary",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "build-type", Sources: cli.EnvVars("BUILD_TYPE")},
			&cli.StringFlag{Name: "group-id", Sources: cli.EnvVars("GROUP_ID")},
			&cli.StringFlag{Name: "artifact-id", Sources: cli.EnvVars("ARTIFACT_ID")},
			&cli.StringFlag{Name: "version", Sources: cli.EnvVars("VERSION")},
			&cli.StringFlag{Name: "java-version", Sources: cli.EnvVars("JAVA_VERSION")},
			&cli.BoolFlag{Name: "skip-tests", Sources: cli.EnvVars("SKIP_TESTS")},
			&cli.BoolFlag{Name: "is-snapshot", Sources: cli.EnvVars("IS_SNAPSHOT")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			for _, f := range []string{"build-type", "group-id", "artifact-id", "version", "java-version"} {
				if cmd.String(f) == "" {
					return fmt.Errorf("--%s is required: %w", f, errs.ErrUsage)
				}
			}
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			return appsummary.MavenBuild(ctx, d.SummarySink, appsummary.MavenBuildInput{
				BuildType:   cmd.String("build-type"),
				GroupID:     cmd.String("group-id"),
				ArtifactID:  cmd.String("artifact-id"),
				Version:     cmd.String("version"),
				JavaVersion: cmd.String("java-version"),
				SkipTests:   cmd.Bool("skip-tests"),
				IsSnapshot:  cmd.Bool("is-snapshot"),
			})
		},
	}
}
