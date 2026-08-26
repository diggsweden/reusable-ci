// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package plan wires `reusable-ci plan ...` subcommands.
package plan

import (
	"context"

	"github.com/urfave/cli/v3"

	appplan "github.com/diggsweden/reusable-ci/v3/internal/app/plan"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
)

// New returns the `plan` subgroup.
func New() *cli.Command {
	return &cli.Command{
		Name:  "plan",
		Usage: "typed release, snapshot-release, and pull-request plan composition",
		Commands: []*cli.Command{
			releaseCmd(),
			snapshotReleaseCmd(),
			prCmd(),
			gitlabBuildPipelineCmd(),
			gitlabPublishPipelineCmd(),
			writeCmd(),
		},
	}
}

func prCmd() *cli.Command {
	return &cli.Command{
		Name:  "pr",
		Usage: "compose typed pull-request quality plan contracts",
		Description: `EXAMPLE:
   reusable-ci plan pr --project-type maven --base-branch main`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project-type", Sources: cli.EnvVars("PROJECT_TYPE"), Usage: "primary ecosystem of the project (maven/npm/go/cargo/…)"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "base-branch", Sources: cli.EnvVars("BASE_BRANCH"), Usage: "base branch the PR targets (used for diff-mode scans)"},
			&cli.StringFlag{Name: "reusable-ci-binary-ref", Sources: cli.EnvVars("REUSABLE_CI_BINARY_REF"), Usage: "git ref of the reusable-ci binary used in the plan (pinned for reproducibility)"},
			&cli.StringFlag{Name: "lint-engine", Sources: cli.EnvVars("LINT_ENGINE"), Usage: "general lint engine to run: nanolinter, megalinter, or none (default none; mutually exclusive)"},
			&cli.BoolFlag{Name: "linter-swiftformat", Sources: cli.EnvVars("LINTER_SWIFTFORMAT"), Usage: "include the swift-format lint gate in the plan"},
			&cli.BoolFlag{Name: "linter-swiftlint", Sources: cli.EnvVars("LINTER_SWIFTLINT"), Usage: "include the swiftlint lint gate in the plan"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				_, err := appplan.PR(ctx, d.OutputSink, appplan.PRInput{
					ProjectType:         cmd.String("project-type"),
					BaseBranch:          cmd.String("base-branch"),
					ReusableCIBinaryRef: cmd.String("reusable-ci-binary-ref"),
					LintEngine:          cmd.String("lint-engine"),
					SwiftFormat:         cmd.Bool("linter-swiftformat"),
					SwiftLint:           cmd.Bool("linter-swiftlint"),
				})

				return err
			})
		},
	}
}

func releaseCmd() *cli.Command {
	return &cli.Command{
		Name:  "release",
		Usage: "compose typed release and stage plan contracts",
		Description: `CONFIG_PLAN_JSON is produced by ` + "`config parse-artifacts`" + `.

EXAMPLE:
   CONFIG_PLAN_JSON="$(reusable-ci config parse-artifacts)" reusable-ci plan release --tag v1.2.3 --branch main`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config-plan-json", Sources: cli.EnvVars("CONFIG_PLAN_JSON"), Usage: "typed config-plan JSON (output of 'config parse-artifacts')"},
			&cli.StringFlag{Name: "branch", Sources: cli.EnvVars("BRANCH"), Usage: "git branch the release is being built from"},
			&cli.StringFlag{Name: "tag", Required: true, Sources: cienv.Tag(), Usage: "release tag (e.g. v1.2.3)"},
			&cli.StringFlag{Name: "file-pattern", Sources: cli.EnvVars("FILE_PATTERN"), Usage: "git pathspecs the version-bump commit stages"},
			&cli.StringFlag{Name: "release-type", Sources: cli.EnvVars("RELEASE_TYPE"), Usage: "type override (release/snapshot); auto-detected from the tag when empty"},
			&cli.StringFlag{Name: "release-publisher", Sources: cli.EnvVars("RELEASE_PUBLISHER"), Usage: "platform that publishes the release (github-cli, gitlab-cli, …)"},
			&cli.BoolFlag{Name: "release-require-allowlisted-signer", Sources: cli.EnvVars("RELEASE_REQUIRE_ALLOWLISTED_SIGNER"), Usage: "require the tag signer to be allowlisted in .reusable-ci/allowed_signers (SSH) or .reusable-ci/allowed_gpg_keys.asc (GPG)"},
			&cli.BoolFlag{Name: "release-draft", Sources: cli.EnvVars("RELEASE_DRAFT"), Usage: "create the GitHub Release as a draft"},
			&cli.StringFlag{Name: "release-sboms", Value: "all", Sources: cli.EnvVars("RELEASE_SBOMS"), Usage: "sboms enum gating which CISA layers the release attaches"},
			&cli.BoolFlag{Name: "release-sign-artifacts", Value: true, Sources: cli.EnvVars("RELEASE_SIGN_ARTIFACTS"), Usage: "sign release artifacts with the release GPG key"},
			&cli.StringFlag{Name: "changelog-creator", Sources: cli.EnvVars("CHANGELOG_CREATOR"), Usage: "tool that generates the changelog (git-cliff, …)"},
			&cli.BoolFlag{Name: "changelog-skip-version-bump", Sources: cli.EnvVars("CHANGELOG_SKIP_VERSION_BUMP"), Usage: "skip the version-bump commit (caller already committed)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				_, err := appplan.Release(ctx, d.OutputSink, d.SummarySink, appplan.ReleaseInput{
					ConfigPlanJSON:                  cmd.String("config-plan-json"),
					Branch:                          cmd.String("branch"),
					RefName:                         cmd.String("tag"),
					FilePattern:                     cmd.String("file-pattern"),
					ReleaseType:                     cmd.String("release-type"),
					ReleasePublisher:                cmd.String("release-publisher"),
					ReleaseRequireAllowlistedSigner: cmd.Bool("release-require-allowlisted-signer"),
					ReleaseDraft:                    cmd.Bool("release-draft"),
					ReleaseSBOMs:                    cmd.String("release-sboms"),
					ReleaseSignArtifacts:            cmd.Bool("release-sign-artifacts"),
					ChangelogCreator:                cmd.String("changelog-creator"),
					ChangelogSkipVersionBump:        cmd.Bool("changelog-skip-version-bump"),
				})

				return err
			})
		},
	}
}

func snapshotReleaseCmd() *cli.Command {
	return &cli.Command{
		Name:  "snapshot-release",
		Usage: "compose typed snapshot-release and stage plan contracts",
		Description: `EXAMPLE:
   CONFIG_PLAN_JSON="$(reusable-ci config parse-artifacts)" \
   reusable-ci plan snapshot-release --project-type npm --branch main`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config-plan-json", Sources: cli.EnvVars("CONFIG_PLAN_JSON"), Usage: "typed config-plan JSON (output of 'config parse-artifacts')"},
			&cli.StringFlag{Name: "project-type", Sources: cli.EnvVars("PROJECT_TYPE"), Usage: "primary ecosystem of the project (maven/npm/go/cargo/…)"},
			&cli.StringFlag{Name: "branch", Sources: cli.EnvVars("BRANCH"), Usage: "git branch the snapshot-release is being built from"},
			&cli.StringFlag{Name: "release-sha", Sources: cli.EnvVars("RELEASE_SHA"), Usage: "commit SHA the snapshot-release is anchored to"},
			&cli.StringFlag{Name: "release-actor", Sources: cli.EnvVars("RELEASE_ACTOR"), Usage: "user triggering the snapshot-release"},
			&cli.StringFlag{Name: "release-repository", Sources: cli.EnvVars("RELEASE_REPOSITORY"), Usage: "\"owner/repo\" the snapshot-release is published from"},
			&cli.StringFlag{Name: "working-dir", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "default working directory when no per-artifact override is in the plan"},
			&cli.StringFlag{Name: "java-version", Sources: cli.EnvVars("JAVA_VERSION"), Usage: "JDK version installed by the publish job (Maven/Gradle paths)"},
			&cli.StringFlag{Name: "node-version", Sources: cli.EnvVars("NODE_VERSION"), Usage: "Node.js version installed by the publish job (npm path)"},
			&cli.StringFlag{Name: "rust-toolchain", Value: "stable", Sources: cli.EnvVars("RUST_TOOLCHAIN"), Usage: "Rust toolchain installed by the publish job (cargo path)"},
			&cli.StringFlag{Name: "registry", Sources: cli.EnvVars("REGISTRY"), Usage: "registry forwarded to the snapshot plan context (snapshot flow is npm/SBOM-only)"},
			&cli.StringFlag{Name: "reusable-ci-binary-ref", Sources: cli.EnvVars("REUSABLE_CI_BINARY_REF"), Usage: "git ref of the reusable-ci binary used in the plan"},
			&cli.StringFlag{Name: "npm-registry", Sources: cli.EnvVars("NPM_REGISTRY"), Usage: "npm registry URL the dev tarball is published to"},
			&cli.StringFlag{Name: "scope", Sources: cli.EnvVars("SCOPE", "PACKAGE_SCOPE"), Usage: "npm package scope (e.g. @examplescope) routed to the registry"},
			&cli.StringFlag{Name: "sboms", Value: "none", Sources: cli.EnvVars("SBOMS"), Usage: "sboms enum gating which CISA layers the snapshot-release produces"},
			&cli.BoolFlag{Name: "publish-npm", Value: true, Sources: cli.EnvVars("PUBLISH_NPM"), Usage: "include the npm dev-publish step in the plan"},
			// Defaults false, unlike publish-npm: the gradle snapshot legs
			// need Central credentials and a signing key, so a caller opts in
			// rather than inheriting a credentialed job by upgrading.
			&cli.BoolFlag{Name: "publish-gradle", Sources: cli.EnvVars("PUBLISH_GRADLE"), Usage: "include the gradle snapshot-publish steps in the plan"},
			&cli.BoolFlag{Name: "use-ci-token", Value: true, Sources: cli.EnvVars("USE_CI_TOKEN"), Usage: "use the CI platform token in place of an explicit registry password"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				_, err := appplan.SnapshotRelease(ctx, d.OutputSink, appplan.SnapshotReleaseInput{
					ConfigPlanJSON:      cmd.String("config-plan-json"),
					ProjectType:         cmd.String("project-type"),
					Branch:              cmd.String("branch"),
					ReleaseSHA:          cmd.String("release-sha"),
					ReleaseActor:        cmd.String("release-actor"),
					ReleaseRepository:   cmd.String("release-repository"),
					WorkingDirectory:    cmd.String("working-dir"),
					JavaVersion:         cmd.String("java-version"),
					NodeVersion:         cmd.String("node-version"),
					RustToolchain:       cmd.String("rust-toolchain"),
					Registry:            cmd.String("registry"),
					ReusableCIBinaryRef: cmd.String("reusable-ci-binary-ref"),
					NPMRegistry:         cmd.String("npm-registry"),
					PackageScope:        cmd.String("scope"),
					SBOMs:               cmd.String("sboms"),
					PublishNPM:          cmd.Bool("publish-npm"),
					PublishGradle:       cmd.Bool("publish-gradle"),
					UseCIToken:          cmd.Bool("use-ci-token"),
				})

				return err
			})
		},
	}
}

// file-pattern moved to `reusable-ci version file-pattern` (it feeds the
// version-bump commit); the pathspec logic stays in app/plan.GetFilePattern.
