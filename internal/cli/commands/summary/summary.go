// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package summary wires `reusable-ci summary ...` subcommands.
package summary

import (
	"context"

	"github.com/urfave/cli/v3"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
)

// New returns the `summary` subgroup.
func New() *cli.Command {
	return &cli.Command{
		Name:  "summary",
		Usage: "stage-result manifest writers + step-summary helpers",
		Commands: []*cli.Command{
			buildStageResultCmd(),
			publishStageResultCmd(),
			prepareStageResultCmd(),
			prQualityStageResultCmd(),
			qualityCheckStatusCmd(),
			prCmd(),
			releaseCmd(),
			prerequisitesCmd(),
			devReleaseCmd(),
			mavenBuildCmd(),
			npmBuildCmd(),
			gradleBuildCmd(),
			androidBuildCmd(),
			xcodeBuildCmd(),
			appStoreUploadCmd(),
			googlePlayUploadCmd(),
			devPublishStageResultCmd(),
		},
	}
}

func buildStageResultCmd() *cli.Command {
	return &cli.Command{
		Name:  "build-stage-result",
		Usage: "compose the build stage manifest + dual-write outputs",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "stage-name", Value: "build", Sources: cli.EnvVars("STAGE_NAME")},
			&cli.StringFlag{Name: "project-type", Sources: cli.EnvVars("PROJECT_TYPE")},
			&cli.StringFlag{Name: "maven-result", Sources: cli.EnvVars("BUILD_MAVEN_RESULT")},
			&cli.StringFlag{Name: "npm-result", Sources: cli.EnvVars("BUILD_NPM_RESULT")},
			&cli.StringFlag{Name: "gradle-result", Sources: cli.EnvVars("BUILD_GRADLE_RESULT")},
			&cli.StringFlag{Name: "gradle-android-result", Sources: cli.EnvVars("BUILD_GRADLE_ANDROID_RESULT")},
			&cli.StringFlag{Name: "xcode-result", Sources: cli.EnvVars("BUILD_XCODE_RESULT")},
			&cli.StringFlag{Name: "maven-artifacts", Value: "[]", Sources: cli.EnvVars("MAVEN_ARTIFACTS")},
			&cli.StringFlag{Name: "npm-artifacts", Value: "[]", Sources: cli.EnvVars("NPM_ARTIFACTS")},
			&cli.StringFlag{Name: "gradle-artifacts", Value: "[]", Sources: cli.EnvVars("GRADLE_ARTIFACTS")},
			&cli.StringFlag{Name: "gradle-android-artifacts", Value: "[]", Sources: cli.EnvVars("GRADLEANDROID_ARTIFACTS")},
			&cli.StringFlag{Name: "xcode-ios-artifacts", Value: "[]", Sources: cli.EnvVars("XCODEIOS_ARTIFACTS")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			_, err = appsummary.BuildStageResult(ctx, d.OutputSink, d.ManifestSink, appsummary.BuildStageInput{
				StageName:              cmd.String("stage-name"),
				ProjectType:            cmd.String("project-type"),
				MavenResult:            cmd.String("maven-result"),
				NPMResult:              cmd.String("npm-result"),
				GradleResult:           cmd.String("gradle-result"),
				GradleAndroidResult:    cmd.String("gradle-android-result"),
				XcodeResult:            cmd.String("xcode-result"),
				MavenArtifacts:         cmd.String("maven-artifacts"),
				NPMArtifacts:           cmd.String("npm-artifacts"),
				GradleArtifacts:        cmd.String("gradle-artifacts"),
				GradleAndroidArtifacts: cmd.String("gradle-android-artifacts"),
				XcodeIOSArtifacts:      cmd.String("xcode-ios-artifacts"),
			})
			return err
		},
	}
}

func publishStageResultCmd() *cli.Command {
	return &cli.Command{
		Name:  "publish-stage-result",
		Usage: "compose the publish stage manifest + dual-write outputs",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "github-packages-result", Sources: cli.EnvVars("PUBLISH_MAVEN_REGISTRY_RESULT")},
			&cli.StringFlag{Name: "maven-central-result", Sources: cli.EnvVars("PUBLISH_MAVEN_CENTRAL_RESULT")},
			&cli.StringFlag{Name: "appstore-result", Sources: cli.EnvVars("PUBLISH_APPLE_APPSTORE_RESULT")},
			&cli.StringFlag{Name: "google-play-result", Sources: cli.EnvVars("PUBLISH_GOOGLE_PLAY_RESULT")},
			&cli.StringFlag{Name: "containers-result", Sources: cli.EnvVars("BUILD_CONTAINERS_RESULT")},
			&cli.StringFlag{Name: "cargo-sbom-result", Sources: cli.EnvVars("CARGO_SBOM_RESULT")},
			&cli.StringFlag{Name: "github-packages-artifacts", Value: "[]", Sources: cli.EnvVars("GITHUBPACKAGES_ARTIFACTS")},
			&cli.StringFlag{Name: "maven-central-artifacts", Value: "[]", Sources: cli.EnvVars("MAVENCENTRAL_ARTIFACTS")},
			&cli.StringFlag{Name: "xcode-ios-artifacts", Value: "[]", Sources: cli.EnvVars("XCODEIOS_ARTIFACTS")},
			&cli.StringFlag{Name: "google-play-artifacts", Value: "[]", Sources: cli.EnvVars("GOOGLEPLAY_ARTIFACTS")},
			&cli.StringFlag{Name: "containers", Value: "[]", Sources: cli.EnvVars("CONTAINERS")},
			&cli.StringFlag{Name: "cargo-artifacts", Value: "[]", Sources: cli.EnvVars("CARGO_ARTIFACTS")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			_, err = appsummary.PublishStageResult(ctx, d.OutputSink, d.ManifestSink, appsummary.PublishStageInput{
				GHPackagesResult:      cmd.String("github-packages-result"),
				MavenCentralResult:    cmd.String("maven-central-result"),
				AppStoreResult:        cmd.String("appstore-result"),
				GooglePlayResult:      cmd.String("google-play-result"),
				ContainersResult:      cmd.String("containers-result"),
				CargoSBOMResult:       cmd.String("cargo-sbom-result"),
				GHPackagesArtifacts:   cmd.String("github-packages-artifacts"),
				MavenCentralArtifacts: cmd.String("maven-central-artifacts"),
				XcodeIOSArtifacts:     cmd.String("xcode-ios-artifacts"),
				GooglePlayArtifacts:   cmd.String("google-play-artifacts"),
				Containers:            cmd.String("containers"),
				CargoArtifacts:        cmd.String("cargo-artifacts"),
			})
			return err
		},
	}
}

func prepareStageResultCmd() *cli.Command {
	return &cli.Command{
		Name:  "prepare-stage-result",
		Usage: "compose the prepare stage manifest + dual-write outputs",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "prepare-release-result", Sources: cli.EnvVars("PREPARE_RELEASE_RESULT")},
			&cli.BoolFlag{Name: "should-run-version-bump", Sources: cli.EnvVars("SHOULD_RUN_VERSION_BUMP")},
			&cli.StringFlag{Name: "artifacts", Value: "[]", Sources: cli.EnvVars("ARTIFACTS")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			_, err = appsummary.PrepareStageResult(ctx, d.OutputSink, d.ManifestSink, appsummary.PrepareStageInput{
				PrepareReleaseResult: cmd.String("prepare-release-result"),
				ShouldRunVersionBump: cmd.Bool("should-run-version-bump"),
				Artifacts:            cmd.String("artifacts"),
			})
			return err
		},
	}
}

func prQualityStageResultCmd() *cli.Command {
	return &cli.Command{
		Name:  "pr-quality-stage-result",
		Usage: "compose the pr-quality stage manifest + dual-write outputs",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "dependency-review-result", Sources: cli.EnvVars("DEPENDENCYREVIEW_RESULT")},
			&cli.StringFlag{Name: "dependency-review-enabled", Sources: cli.EnvVars("DEPENDENCYREVIEW_ENABLED")},
			&cli.StringFlag{Name: "sast-opengrep-result", Sources: cli.EnvVars("SASTOPENGREP_RESULT")},
			&cli.StringFlag{Name: "sast-opengrep-enabled", Sources: cli.EnvVars("SASTOPENGREP_ENABLED")},
			&cli.StringFlag{Name: "publiccodelint-result", Sources: cli.EnvVars("PUBLICCODELINT_RESULT")},
			&cli.StringFlag{Name: "publiccodelint-enabled", Sources: cli.EnvVars("PUBLICCODELINT_ENABLED")},
			&cli.StringFlag{Name: "devbasecheck-result", Sources: cli.EnvVars("DEVBASECHECK_RESULT")},
			&cli.StringFlag{Name: "devbasecheck-enabled", Sources: cli.EnvVars("DEVBASECHECK_ENABLED")},
			&cli.StringFlag{Name: "swift-result", Sources: cli.EnvVars("SWIFT_RESULT")},
			&cli.StringFlag{Name: "swift-enabled", Sources: cli.EnvVars("SWIFT_ENABLED")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			_, err = appsummary.PRQualityStageResult(ctx, d.OutputSink, d.ManifestSink, appsummary.PRQualityStageInput{
				DependencyReviewResult:  cmd.String("dependency-review-result"),
				DependencyReviewEnabled: cmd.String("dependency-review-enabled"),
				SASTOpengrepResult:      cmd.String("sast-opengrep-result"),
				SASTOpengrepEnabled:     cmd.String("sast-opengrep-enabled"),
				PublicCodeLintResult:    cmd.String("publiccodelint-result"),
				PublicCodeLintEnabled:   cmd.String("publiccodelint-enabled"),
				DevbaseCheckResult:      cmd.String("devbasecheck-result"),
				DevbaseCheckEnabled:     cmd.String("devbasecheck-enabled"),
				SwiftResult:             cmd.String("swift-result"),
				SwiftEnabled:            cmd.String("swift-enabled"),
			})
			return err
		},
	}
}
