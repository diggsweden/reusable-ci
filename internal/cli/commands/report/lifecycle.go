// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package report

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
)

// readJSONInput honours the convention: inline JSON on its own, or
// read-from-file when only the path is set, or empty when neither.
func readJSONInput(inline, path string) (string, error) {
	if path != "" {
		data, err := os.ReadFile(path) //nolint:gosec // path is a CLI-flag value.
		if err != nil {
			return "", fmt.Errorf("read %q: %w", path, err)
		}

		return strings.TrimSpace(string(data)), nil
	}

	return inline, nil
}

func prCmd() *cli.Command {
	return &cli.Command{
		Name:  "pr",
		Usage: "append the PR step-summary (quality table + run link)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project-type", Sources: cli.EnvVars("PROJECT_TYPE"), Usage: "primary ecosystem of the project (shown in the header)"},
			&cli.StringFlag{Name: "branch", Sources: cli.EnvVars("CI_BRANCH"), Usage: "PR source branch"},
			&cli.StringFlag{Name: "commit", Sources: cli.EnvVars("CI_COMMIT"), Usage: "head commit SHA of the PR"},
			&cli.StringFlag{Name: "actor", Sources: cli.EnvVars("CI_ACTOR"), Usage: "user who opened/updated the PR"},
			&cli.StringFlag{Name: "run-url", Sources: cli.EnvVars("CI_RUN_URL"), Usage: "URL of the CI run linked from the summary"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "quality-stage-result-json", Sources: cli.EnvVars("QUALITY_STAGE_RESULT_JSON"), Usage: "inline JSON of the quality-stage result table"},
			&cli.StringFlag{Name: "quality-stage-result-path", Sources: cli.EnvVars("QUALITY_STAGE_RESULT_PATH"), Usage: "path to the quality-stage result JSON file (alternative to inline)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			qsr, err := readJSONInput(
				cmd.String("quality-stage-result-json"),
				cmd.String("quality-stage-result-path"),
			)
			if err != nil {
				return err
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appsummary.PRSummary(ctx, d.SummarySink, appsummary.PRSummaryInput{
					ProjectType:            cmd.String("project-type"),
					Branch:                 cmd.String("branch"),
					Commit:                 cmd.String("commit"),
					Actor:                  cmd.String("actor"),
					RunURL:                 cmd.String("run-url"),
					QualityStageResultJSON: qsr,
				})
			})
		},
	}
}

func releaseCmd() *cli.Command {
	return &cli.Command{
		Name:  "release",
		Usage: "append the release step-summary (job table + release/packages/run links)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "release-version", Sources: cli.EnvVars("RELEASE_VERSION"), Usage: "release version (e.g. v1.2.3)"},
			&cli.StringFlag{Name: "release-branch", Sources: cli.EnvVars("RELEASE_BRANCH"), Usage: "branch the release was cut from"},
			&cli.StringFlag{Name: "release-commit", Sources: cli.EnvVars("RELEASE_COMMIT"), Usage: "commit SHA the release was cut from"},
			&cli.StringFlag{Name: "release-actor", Sources: cli.EnvVars("RELEASE_ACTOR"), Usage: "user who triggered the release"},
			&cli.StringFlag{Name: "run-url", Sources: cli.EnvVars("CI_RUN_URL"), Usage: "URL of the CI run linked from the summary"},
			&cli.StringFlag{Name: "create-release-result", Sources: cli.EnvVars("CREATE_RELEASE_RESULT"), Usage: "outcome of the create-release step (success/failure/skipped)"},
			&cli.StringFlag{Name: "prepare-stage-result-json", Sources: cli.EnvVars("PREPARE_STAGE_RESULT_JSON"), Usage: "inline JSON of the prepare-stage result table"},
			&cli.StringFlag{Name: "build-stage-result-json", Sources: cli.EnvVars("BUILD_STAGE_RESULT_JSON"), Usage: "inline JSON of the build-stage result table"},
			&cli.StringFlag{Name: "publish-stage-result-json", Sources: cli.EnvVars("PUBLISH_STAGE_RESULT_JSON"), Usage: "inline JSON of the publish-stage result table"},
			&cli.StringFlag{Name: "server-url", Sources: cli.EnvVars("CI_SERVER_URL"), Usage: "CI server base URL (used to build release/run links)"},
			&cli.StringFlag{Name: "repository", Sources: cli.EnvVars("CI_REPO", "GITHUB_REPOSITORY"), Usage: "\"owner/repo\" used in the release link"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				return appsummary.ReleaseSummary(ctx, d.SummarySink, appsummary.ReleaseSummaryInput{
					ReleaseVersion:      cmd.String("release-version"),
					ReleaseBranch:       cmd.String("release-branch"),
					ReleaseCommit:       cmd.String("release-commit"),
					ReleaseActor:        cmd.String("release-actor"),
					RunURL:              cmd.String("run-url"),
					CreateReleaseResult: cmd.String("create-release-result"),
					PrepareStageJSON:    cmd.String("prepare-stage-result-json"),
					BuildStageJSON:      cmd.String("build-stage-result-json"),
					PublishStageJSON:    cmd.String("publish-stage-result-json"),
					Platform:            d.Platform,
					ServerURL:           cmd.String("server-url"),
					Repository:          cmd.String("repository"),
				})
			})
		},
	}
}

func devReleaseCmd() *cli.Command {
	return &cli.Command{
		Name:  "dev-release",
		Usage: "append the dev-release step-summary (job table, npm install snippet, and links)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project-type", Sources: cli.EnvVars("PROJECT_TYPE"), Usage: "primary ecosystem (drives the npm-install snippet in the summary)"},
			&cli.StringFlag{Name: "release-ref", Sources: cli.EnvVars("RELEASE_REF"), Usage: "git ref the dev-release was cut from"},
			&cli.StringFlag{Name: "release-sha", Sources: cli.EnvVars("RELEASE_SHA"), Usage: "commit SHA the dev-release was cut from"},
			&cli.StringFlag{Name: "release-actor", Sources: cli.EnvVars("RELEASE_ACTOR"), Usage: "user who triggered the dev-release"},
			&cli.StringFlag{Name: "release-repository", Sources: cli.EnvVars("RELEASE_REPOSITORY"), Usage: "\"owner/repo\" the dev-release was published from"},
			&cli.StringFlag{Name: "run-url", Sources: cli.EnvVars("CI_RUN_URL"), Usage: "URL of the CI run linked from the summary"},
			&cli.StringFlag{Name: "build-stage-result-json", Sources: cli.EnvVars("BUILD_STAGE_RESULT_JSON"), Usage: "inline JSON of the dev-build stage result table"},
			&cli.StringFlag{Name: "publish-stage-result-json", Sources: cli.EnvVars("PUBLISH_STAGE_RESULT_JSON"), Usage: "inline JSON of the dev-publish stage result table"},
			&cli.StringFlag{Name: "dev-artifacts-json", Sources: cli.EnvVars("DEV_ARTIFACTS_JSON"), Usage: "inline JSON listing the dev artifacts shown in the summary"},
			&cli.StringFlag{Name: "server-url", Sources: cli.EnvVars("CI_SERVER_URL"), Usage: "CI server base URL (used to build artifact/run links)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				return appsummary.DevReleaseSummary(ctx, d.SummarySink, os.Stderr, appsummary.DevReleaseSummaryInput{
					ProjectType:       projecttype.Type(cmd.String("project-type")),
					ReleaseRef:        cmd.String("release-ref"),
					ReleaseSHA:        cmd.String("release-sha"),
					ReleaseActor:      cmd.String("release-actor"),
					ReleaseRepository: cmd.String("release-repository"),
					RunURL:            cmd.String("run-url"),
					BuildStageJSON:    cmd.String("build-stage-result-json"),
					PublishStageJSON:  cmd.String("publish-stage-result-json"),
					DevArtifactsJSON:  cmd.String("dev-artifacts-json"),
					Platform:          d.Platform,
					ServerURL:         cmd.String("server-url"),
				})
			})
		},
	}
}
