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
)

// New returns the `plan` subgroup.
func New() *cli.Command {
	return &cli.Command{
		Name:  "plan",
		Usage: "typed release, dev-release, and pull-request plan composition",
		Commands: []*cli.Command{
			releaseCmd(),
			devReleaseCmd(),
			prCmd(),
			getFilePatternCmd(),
		},
	}
}

func prCmd() *cli.Command {
	return &cli.Command{
		Name:  "pr",
		Usage: "compose typed pull-request quality plan contracts",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project-type", Sources: cli.EnvVars("PROJECT_TYPE"), Usage: "primary ecosystem of the project (maven/npm/go/cargo/…)"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "base-branch", Sources: cli.EnvVars("BASE_BRANCH"), Usage: "base branch the PR targets (used for diff-mode scans)"},
			&cli.StringFlag{Name: "reusable-ci-binary-ref", Sources: cli.EnvVars("REUSABLE_CI_BINARY_REF"), Usage: "git ref of the reusable-ci binary used in the plan (pinned for reproducibility)"},
			&cli.StringFlag{Name: "sast-opengrep-rules", Value: "p/default", Sources: cli.EnvVars("SAST_OPENGREP_RULES"), Usage: "comma-separated opengrep rulesets the SAST quality gate uses"},
			&cli.StringFlag{Name: "sast-opengrep-fail-on-severity", Value: "high", Sources: cli.EnvVars("SAST_OPENGREP_FAIL_ON_SEVERITY"), Usage: "minimum opengrep severity that fails the SAST gate"},
			&cli.BoolFlag{Name: "linter-dependencyreview", Sources: cli.EnvVars("LINTER_DEPENDENCYREVIEW"), Usage: "include the GitHub dependency-review gate in the plan"},
			&cli.BoolFlag{Name: "sast-opengrep", Sources: cli.EnvVars("SAST_OPENGREP"), Usage: "include the opengrep SAST gate in the plan"},
			&cli.BoolFlag{Name: "linter-publiccodelint", Sources: cli.EnvVars("LINTER_PUBLICCODELINT"), Usage: "include the publiccode-yml lint gate in the plan"},
			&cli.BoolFlag{Name: "linter-devbasecheck", Sources: cli.EnvVars("LINTER_DEVBASECHECK"), Usage: "include the devbase lint gate in the plan"},
			&cli.BoolFlag{Name: "linter-nanolinter", Sources: cli.EnvVars("LINTER_NANOLINTER"), Usage: "include the nanolinter lint gate in the plan"},
			&cli.BoolFlag{Name: "linter-swiftformat", Sources: cli.EnvVars("LINTER_SWIFTFORMAT"), Usage: "include the swift-format lint gate in the plan"},
			&cli.BoolFlag{Name: "linter-swiftlint", Sources: cli.EnvVars("LINTER_SWIFTLINT"), Usage: "include the swiftlint lint gate in the plan"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				_, err := appplan.PR(ctx, d.OutputSink, appplan.PRInput{
					ProjectType:                cmd.String("project-type"),
					BaseBranch:                 cmd.String("base-branch"),
					ReusableCIBinaryRef:        cmd.String("reusable-ci-binary-ref"),
					SASTOpengrepRules:          cmd.String("sast-opengrep-rules"),
					SASTOpengrepFailOnSeverity: cmd.String("sast-opengrep-fail-on-severity"),
					DependencyReview:           cmd.Bool("linter-dependencyreview"),
					SASTOpengrep:               cmd.Bool("sast-opengrep"),
					PublicCodeLint:             cmd.Bool("linter-publiccodelint"),
					DevbaseCheck:               cmd.Bool("linter-devbasecheck"),
					Nanolinter:                 cmd.Bool("linter-nanolinter"),
					SwiftFormat:                cmd.Bool("linter-swiftformat"),
					SwiftLint:                  cmd.Bool("linter-swiftlint"),
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
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config-plan-json", Sources: cli.EnvVars("CONFIG_PLAN_JSON"), Usage: "typed config-plan JSON (output of 'config parse-artifacts')"},
			&cli.StringFlag{Name: "branch", Sources: cli.EnvVars("BRANCH"), Usage: "git branch the release is being built from"},
			&cli.StringFlag{Name: "ref-name", Required: true, Sources: cli.EnvVars("CI_REF_NAME", "GITHUB_REF_NAME"), Usage: "release tag (e.g. v1.2.3)"},
			&cli.StringFlag{Name: "file-pattern", Sources: cli.EnvVars("FILE_PATTERN"), Usage: "git pathspecs the version-bump commit stages"},
			&cli.StringFlag{Name: "release-type", Sources: cli.EnvVars("RELEASE_TYPE"), Usage: "type override (release/snapshot); auto-detected from the tag when empty"},
			&cli.StringFlag{Name: "release-publisher", Sources: cli.EnvVars("RELEASE_PUBLISHER"), Usage: "platform that publishes the release (github-cli, gitlab-cli, …)"},
			&cli.BoolFlag{Name: "release-require-allowlisted-signer", Sources: cli.EnvVars("RELEASE_REQUIRE_ALLOWLISTED_SIGNER"), Usage: "require the tag signer's fingerprint to appear in .reusable-ci/allowed_signers (SSH) or .reusable-ci/allowed_gpg_fingerprints (GPG)"},
			&cli.BoolFlag{Name: "release-draft", Sources: cli.EnvVars("RELEASE_DRAFT"), Usage: "create the GitHub Release as a draft"},
			&cli.StringFlag{Name: "release-sboms", Value: "all", Sources: cli.EnvVars("RELEASE_SBOMS"), Usage: "sboms enum gating which CISA layers the release attaches"},
			&cli.BoolFlag{Name: "release-sign-artifacts", Value: true, Sources: cli.EnvVars("RELEASE_SIGN_ARTIFACTS"), Usage: "sign release artifacts with the release GPG key"},
			&cli.StringFlag{Name: "changelog-creator", Sources: cli.EnvVars("CHANGELOG_CREATOR"), Usage: "tool that generates the changelog (git-cliff, …)"},
			&cli.BoolFlag{Name: "changelog-skip-version-bump", Sources: cli.EnvVars("CHANGELOG_SKIP_VERSION_BUMP"), Usage: "skip the version-bump commit (caller already committed)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				_, err := appplan.Release(ctx, d.OutputSink, d.SummarySink, appplan.ReleaseInput{
					ConfigPlanJSON:            cmd.String("config-plan-json"),
					Branch:                    cmd.String("branch"),
					RefName:                   cmd.String("ref-name"),
					FilePattern:               cmd.String("file-pattern"),
					ReleaseType:               cmd.String("release-type"),
					ReleasePublisher:          cmd.String("release-publisher"),
					ReleaseRequireAllowlistedSigner: cmd.Bool("release-require-allowlisted-signer"),
					ReleaseDraft:              cmd.Bool("release-draft"),
					ReleaseSBOMs:              cmd.String("release-sboms"),
					ReleaseSignArtifacts:      cmd.Bool("release-sign-artifacts"),
					ChangelogCreator:          cmd.String("changelog-creator"),
					ChangelogSkipVersionBump:  cmd.Bool("changelog-skip-version-bump"),
				})

				return err
			})
		},
	}
}

func devReleaseCmd() *cli.Command {
	return &cli.Command{
		Name:  "dev-release",
		Usage: "compose typed dev-release and stage plan contracts",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config-plan-json", Sources: cli.EnvVars("CONFIG_PLAN_JSON"), Usage: "typed config-plan JSON (output of 'config parse-artifacts')"},
			&cli.StringFlag{Name: "project-type", Sources: cli.EnvVars("PROJECT_TYPE"), Usage: "primary ecosystem of the project (maven/npm/go/cargo/…)"},
			&cli.StringFlag{Name: "branch", Sources: cli.EnvVars("BRANCH"), Usage: "git branch the dev-release is being built from"},
			&cli.StringFlag{Name: "release-sha", Sources: cli.EnvVars("RELEASE_SHA"), Usage: "commit SHA the dev-release is anchored to"},
			&cli.StringFlag{Name: "release-actor", Sources: cli.EnvVars("RELEASE_ACTOR"), Usage: "user triggering the dev-release"},
			&cli.StringFlag{Name: "release-repository", Sources: cli.EnvVars("RELEASE_REPOSITORY"), Usage: "\"owner/repo\" the dev-release is published from"},
			&cli.StringFlag{Name: "working-dir", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "default working directory when no per-artifact override is in the plan"},
			&cli.StringFlag{Name: "java-version", Sources: cli.EnvVars("JAVA_VERSION"), Usage: "JDK version installed by the publish job (Maven/Gradle paths)"},
			&cli.StringFlag{Name: "node-version", Sources: cli.EnvVars("NODE_VERSION"), Usage: "Node.js version installed by the publish job (npm path)"},
			&cli.StringFlag{Name: "rust-toolchain", Value: "stable", Sources: cli.EnvVars("RUST_TOOLCHAIN"), Usage: "Rust toolchain installed by the publish job (cargo path)"},
			&cli.StringFlag{Name: "registry", Sources: cli.EnvVars("REGISTRY"), Usage: "container registry the dev image is pushed to"},
			&cli.StringFlag{Name: "reusable-ci-binary-ref", Sources: cli.EnvVars("REUSABLE_CI_BINARY_REF"), Usage: "git ref of the reusable-ci binary used in the plan"},
			&cli.StringFlag{Name: "npm-registry", Sources: cli.EnvVars("NPM_REGISTRY"), Usage: "npm registry URL the dev tarball is published to"},
			&cli.StringFlag{Name: "package-scope", Sources: cli.EnvVars("PACKAGE_SCOPE"), Usage: "npm package scope (e.g. @diggsweden) routed to the registry"},
			&cli.StringFlag{Name: "sboms", Value: "none", Sources: cli.EnvVars("SBOMS"), Usage: "sboms enum gating which CISA layers the dev-release produces"},
			&cli.BoolFlag{Name: "publish-npm", Value: true, Sources: cli.EnvVars("PUBLISH_NPM"), Usage: "include the npm dev-publish step in the plan"},
			&cli.BoolFlag{Name: "use-ci-token", Value: true, Sources: cli.EnvVars("USE_CI_TOKEN"), Usage: "use the CI platform token in place of an explicit registry password"},
			&cli.BoolFlag{Name: "publish-container", Value: true, Sources: cli.EnvVars("PUBLISH_CONTAINER"), Usage: "include the container dev-publish step in the plan"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				_, err := appplan.DevRelease(ctx, d.OutputSink, appplan.DevReleaseInput{
					ConfigPlanJSON:      cmd.String("config-plan-json"),
					ProjectType:         cmd.String("project-type"),
					Branch:              cmd.String("branch"),
					ReleaseSHA:          cmd.String("release-sha"),
					ReleaseActor:        cmd.String("release-actor"),
					ReleaseRepository:   cmd.String("release-repository"),
					WorkingDirectory:    cmd.String("working-directory"),
					JavaVersion:         cmd.String("java-version"),
					NodeVersion:         cmd.String("node-version"),
					RustToolchain:       cmd.String("rust-toolchain"),
					Registry:            cmd.String("registry"),
					ReusableCIBinaryRef: cmd.String("reusable-ci-binary-ref"),
					NPMRegistry:         cmd.String("npm-registry"),
					PackageScope:        cmd.String("package-scope"),
					SBOMs:               cmd.String("sboms"),
					PublishNPM:          cmd.Bool("publish-npm"),
					UseCIToken:          cmd.Bool("use-ci-token"),
					PublishContainer:    cmd.Bool("publish-container"),
				})

				return err
			})
		},
	}
}

func getFilePatternCmd() *cli.Command {
	return &cli.Command{
		Name:  "file-pattern",
		Usage: "print the version-bump pathspec for a project type",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "project-type",
				Sources: cli.EnvVars("PROJECT_TYPE"),
				Usage:   "ecosystem whose default pathspec to emit (ignored if --custom-pattern is set)",
			},
			&cli.StringFlag{
				Name:    "custom-pattern",
				Sources: cli.EnvVars("EXPLICIT_FILE_PATTERN"),
				Usage:   "verbatim pathspec to emit, overriding the ecosystem default",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			format, err := deps.OutputFormat(cmd)
			if err != nil {
				return err
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				_, err := appplan.GetFilePattern(ctx, d.OutputSink, os.Stderr, appplan.GetFilePatternInput{
					ProjectType:   cmd.String("project-type"),
					CustomPattern: cmd.String("custom-pattern"),
					WriteToOutput: true,
					Format:        format,
				})

				return err
			})
		},
	}
}
