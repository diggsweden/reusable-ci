// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package plan wires `reusable-ci plan ...` subcommands.
package plan

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appplan "github.com/diggsweden/reusable-ci/internal/app/plan"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	domainplan "github.com/diggsweden/reusable-ci/internal/domain/plan"
)

// New returns the `plan` subgroup.
func New() *cli.Command {
	return &cli.Command{
		Name:  "plan",
		Usage: "release-policy resolution and stage interface composition",
		Commands: []*cli.Command{
			resolveReleasePlanCmd(),
			writeReleaseInterfaceCmd(),
			writeDevReleaseInterfaceCmd(),
			writePRInterfaceCmd(),
			getFilePatternCmd(),
		},
	}
}

func resolveReleasePlanCmd() *cli.Command {
	return &cli.Command{
		Name:  "resolve-release-plan",
		Usage: "compute release-policy booleans + effective SBOMs from inputs",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "release-type", Sources: cli.EnvVars("RELEASE_TYPE")},
			&cli.StringFlag{Name: "release-publisher", Sources: cli.EnvVars("RELEASE_PUBLISHER")},
			&cli.BoolFlag{Name: "release-check-authorization", Sources: cli.EnvVars("RELEASE_CHECK_AUTHORIZATION")},
			&cli.BoolFlag{Name: "release-draft", Sources: cli.EnvVars("RELEASE_DRAFT")},
			&cli.StringFlag{Name: "release-sboms", Value: "all", Sources: cli.EnvVars("RELEASE_SBOMS")},
			&cli.BoolFlag{Name: "release-sign-artifacts", Sources: cli.EnvVars("RELEASE_SIGN_ARTIFACTS")},
			&cli.StringFlag{Name: "changelog-creator", Sources: cli.EnvVars("CHANGELOG_CREATOR")},
			&cli.BoolFlag{Name: "changelog-skip-version-bump", Sources: cli.EnvVars("CHANGELOG_SKIP_VERSION_BUMP")},
			&cli.StringFlag{Name: "ref-name", Required: true, Sources: cli.EnvVars("CI_REF_NAME", "GITHUB_REF_NAME")},
			&cli.StringFlag{Name: "pipeline-sboms", Value: "none", Sources: cli.EnvVars("PIPELINE_SBOMS")},
			&cli.BoolFlag{Name: "any-require-authorization", Sources: cli.EnvVars("ANY_REQUIRE_AUTHORIZATION")},
			&cli.StringFlag{Name: "containers", Value: "[]", Sources: cli.EnvVars("CONTAINERS")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			_, err = appplan.ResolveReleasePlan(ctx, d.OutputSink, d.SummarySink, domainplan.ReleasePlanInputs{
				ReleaseType:               cmd.String("release-type"),
				ReleasePublisher:          cmd.String("release-publisher"),
				ReleaseCheckAuthorization: cmd.Bool("release-check-authorization"),
				ReleaseDraft:              cmd.Bool("release-draft"),
				ReleaseSBOMs:              cmd.String("release-sboms"),
				ReleaseSignArtifacts:      cmd.Bool("release-sign-artifacts"),
				ChangelogCreator:          cmd.String("changelog-creator"),
				ChangelogSkipVersionBump:  cmd.Bool("changelog-skip-version-bump"),
				RefName:                   cmd.String("ref-name"),
				PipelineSBOMs:             cmd.String("pipeline-sboms"),
				AnyRequireAuthorization:   cmd.Bool("any-require-authorization"),
				HasContainers:             cmd.String("containers") != "[]",
			})
			return err
		},
	}
}

func writeReleaseInterfaceCmd() *cli.Command {
	return &cli.Command{
		Name:  "write-release-interface",
		Usage: "compose the release-policy-json envelope from pre-resolved booleans",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "sign-artifacts", Sources: cli.EnvVars("SHOULD_SIGN_ARTIFACTS")},
			&cli.BoolFlag{Name: "check-authorization", Sources: cli.EnvVars("SHOULD_CHECK_AUTHORIZATION")},
			&cli.BoolFlag{Name: "run-version-bump", Sources: cli.EnvVars("SHOULD_RUN_VERSION_BUMP")},
			&cli.BoolFlag{Name: "create-release", Sources: cli.EnvVars("SHOULD_CREATE_RELEASE")},
			&cli.BoolFlag{Name: "create-draft-release", Sources: cli.EnvVars("SHOULD_CREATE_DRAFT_RELEASE")},
			&cli.StringFlag{Name: "sboms", Value: "none", Sources: cli.EnvVars("EFFECTIVE_SBOMS")},
			&cli.BoolFlag{Name: "make-latest", Sources: cli.EnvVars("SHOULD_MAKE_LATEST")},
			&cli.BoolFlag{Name: "has-containers", Sources: cli.EnvVars("HAS_CONTAINERS")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			return appplan.WriteReleaseInterface(ctx, d.OutputSink, appplan.WriteReleaseInterfaceInput{
				SignArtifacts:      cmd.Bool("sign-artifacts"),
				CheckAuthorization: cmd.Bool("check-authorization"),
				RunVersionBump:     cmd.Bool("run-version-bump"),
				CreateRelease:      cmd.Bool("create-release"),
				CreateDraftRelease: cmd.Bool("create-draft-release"),
				SBOMs:              cmd.String("sboms"),
				MakeLatest:         cmd.Bool("make-latest"),
				HasContainers:      cmd.Bool("has-containers"),
			})
		},
	}
}

func writeDevReleaseInterfaceCmd() *cli.Command {
	return &cli.Command{
		Name:  "write-dev-release-interface",
		Usage: "compose dev-context-json + dev-policy-json for the dev-release stage",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project-type", Sources: cli.EnvVars("PROJECT_TYPE")},
			&cli.StringFlag{Name: "fallback-project-type", Sources: cli.EnvVars("FALLBACK_PROJECT_TYPE")},
			&cli.StringFlag{Name: "branch", Sources: cli.EnvVars("BRANCH")},
			&cli.StringFlag{Name: "release-sha", Sources: cli.EnvVars("RELEASE_SHA")},
			&cli.StringFlag{Name: "release-actor", Sources: cli.EnvVars("RELEASE_ACTOR")},
			&cli.StringFlag{Name: "release-repository", Sources: cli.EnvVars("RELEASE_REPOSITORY")},
			&cli.StringFlag{Name: "working-directory", Sources: cli.EnvVars("WORKING_DIRECTORY")},
			&cli.StringFlag{Name: "java-version", Sources: cli.EnvVars("JAVA_VERSION")},
			&cli.StringFlag{Name: "node-version", Sources: cli.EnvVars("NODE_VERSION")},
			&cli.StringFlag{Name: "rust-toolchain", Value: "stable", Sources: cli.EnvVars("RUST_TOOLCHAIN")},
			&cli.StringFlag{Name: "registry", Sources: cli.EnvVars("REGISTRY")},
			&cli.StringFlag{Name: "scripts-ref", Sources: cli.EnvVars("SCRIPTS_REF")},
			&cli.StringFlag{Name: "npm-registry", Sources: cli.EnvVars("NPM_REGISTRY")},
			&cli.StringFlag{Name: "package-scope", Sources: cli.EnvVars("PACKAGE_SCOPE")},
			&cli.BoolFlag{Name: "publish-npm", Sources: cli.EnvVars("PUBLISH_NPM")},
			&cli.BoolFlag{Name: "use-ci-token", Sources: cli.EnvVars("USE_CI_TOKEN")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			pt := cmd.String("project-type")
			if pt == "" {
				pt = cmd.String("fallback-project-type")
			}
			return appplan.WriteDevReleaseInterface(ctx, d.OutputSink, appplan.WriteDevReleaseInterfaceInput{
				DevContext: domainplan.DevContext{
					ProjectType:       pt,
					Branch:            cmd.String("branch"),
					ReleaseSHA:        cmd.String("release-sha"),
					ReleaseActor:      cmd.String("release-actor"),
					ReleaseRepository: cmd.String("release-repository"),
					WorkingDirectory:  cmd.String("working-directory"),
					JavaVersion:       cmd.String("java-version"),
					NodeVersion:       cmd.String("node-version"),
					RustToolchain:     cmd.String("rust-toolchain"),
					Registry:          cmd.String("registry"),
					ScriptsRef:        cmd.String("scripts-ref"),
					NPMRegistry:       cmd.String("npm-registry"),
					PackageScope:      cmd.String("package-scope"),
				},
				DevPolicy: domainplan.DevPolicy{
					PublishNPM: cmd.Bool("publish-npm"),
					UseCIToken: cmd.Bool("use-ci-token"),
				},
			})
		},
	}
}

func writePRInterfaceCmd() *cli.Command {
	return &cli.Command{
		Name:  "write-pr-interface",
		Usage: "compose pr-context-json + pr-policy-json for the PR stage",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project-type", Sources: cli.EnvVars("PROJECT_TYPE")},
			&cli.StringFlag{Name: "base-branch", Sources: cli.EnvVars("BASE_BRANCH")},
			&cli.StringFlag{Name: "scripts-ref", Sources: cli.EnvVars("SCRIPTS_REF")},
			&cli.StringFlag{Name: "sast-opengrep-rules", Value: "p/default", Sources: cli.EnvVars("SAST_OPENGREP_RULES")},
			&cli.StringFlag{Name: "sast-opengrep-fail-on-severity", Value: "high", Sources: cli.EnvVars("SAST_OPENGREP_FAIL_ON_SEVERITY")},
			&cli.BoolFlag{Name: "linter-dependencyreview", Sources: cli.EnvVars("LINTER_DEPENDENCYREVIEW")},
			&cli.BoolFlag{Name: "sast-opengrep", Sources: cli.EnvVars("SAST_OPENGREP")},
			&cli.BoolFlag{Name: "linter-publiccodelint", Sources: cli.EnvVars("LINTER_PUBLICCODELINT")},
			&cli.BoolFlag{Name: "linter-devbasecheck", Sources: cli.EnvVars("LINTER_DEVBASECHECK")},
			&cli.BoolFlag{Name: "linter-swiftformat", Sources: cli.EnvVars("LINTER_SWIFTFORMAT")},
			&cli.BoolFlag{Name: "linter-swiftlint", Sources: cli.EnvVars("LINTER_SWIFTLINT")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			return appplan.WritePRInterface(ctx, d.OutputSink, appplan.WritePRInterfaceInput{
				PRContext: domainplan.PRContext{
					ProjectType:                cmd.String("project-type"),
					BaseBranch:                 cmd.String("base-branch"),
					ScriptsRef:                 cmd.String("scripts-ref"),
					SASTOpengrepRules:          cmd.String("sast-opengrep-rules"),
					SASTOpengrepFailOnSeverity: cmd.String("sast-opengrep-fail-on-severity"),
				},
				PRPolicy: domainplan.PRPolicy{
					DependencyReview: cmd.Bool("linter-dependencyreview"),
					SASTOpengrep:     cmd.Bool("sast-opengrep"),
					PublicCodeLint:   cmd.Bool("linter-publiccodelint"),
					DevbaseCheck:     cmd.Bool("linter-devbasecheck"),
					SwiftFormat:      cmd.Bool("linter-swiftformat"),
					SwiftLint:        cmd.Bool("linter-swiftlint"),
				},
			})
		},
	}
}

func getFilePatternCmd() *cli.Command {
	return &cli.Command{
		Name:      "get-file-pattern",
		Usage:     "print the version-bump pathspec for a project type",
		ArgsUsage: "<project-type> [custom-pattern]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project-type", Sources: cli.EnvVars("PROJECT_TYPE")},
			&cli.StringFlag{Name: "custom-pattern", Sources: cli.EnvVars("EXPLICIT_FILE_PATTERN")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			args := cmd.Args().Slice()
			pt := cmd.String("project-type")
			cp := cmd.String("custom-pattern")
			writeOutput := false
			if len(args) >= 1 {
				pt = args[0]
			} else {
				writeOutput = true
			}
			if len(args) >= 2 {
				cp = args[1]
			}
			_, err = appplan.GetFilePattern(ctx, d.OutputSink, os.Stdout, appplan.GetFilePatternInput{
				ProjectType:   pt,
				CustomPattern: cp,
				WriteToOutput: writeOutput,
			})
			return err
		},
	}
}
