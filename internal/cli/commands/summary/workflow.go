// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
)

func qualityCheckStatusCmd() *cli.Command {
	return &cli.Command{
		Name:      "quality-check-status",
		Usage:     "append a PR-quality summary block from \"Name|enabled|result\" args",
		ArgsUsage: "<Name|enabled|result> ...",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			checks := appsummary.ParseQualityChecks(cmd.Args().Slice())
			return appsummary.QualityCheckStatus(ctx, d.SummarySink, checks)
		},
	}
}

// readJSONInput honours the bash convention: inline JSON on its own,
// or read-from-file when only the path is set, or empty when neither.
func readJSONInput(inline, path string) (string, error) {
	if path != "" {
		data, err := os.ReadFile(path)
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
			&cli.StringFlag{Name: "project-type", Sources: cli.EnvVars("PROJECT_TYPE")},
			&cli.StringFlag{Name: "branch", Sources: cli.EnvVars("CI_BRANCH")},
			&cli.StringFlag{Name: "commit", Sources: cli.EnvVars("CI_COMMIT")},
			&cli.StringFlag{Name: "actor", Sources: cli.EnvVars("CI_ACTOR")},
			&cli.StringFlag{Name: "run-url", Sources: cli.EnvVars("CI_RUN_URL")},
			&cli.StringFlag{Name: "quality-stage-result-json", Sources: cli.EnvVars("QUALITY_STAGE_RESULT_JSON")},
			&cli.StringFlag{Name: "quality-stage-result-path", Sources: cli.EnvVars("QUALITY_STAGE_RESULT_PATH")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			qsr, err := readJSONInput(
				cmd.String("quality-stage-result-json"),
				cmd.String("quality-stage-result-path"),
			)
			if err != nil {
				return err
			}
			return appsummary.PRSummary(ctx, d.SummarySink, appsummary.PRSummaryInput{
				ProjectType:            cmd.String("project-type"),
				Branch:                 cmd.String("branch"),
				Commit:                 cmd.String("commit"),
				Actor:                  cmd.String("actor"),
				RunURL:                 cmd.String("run-url"),
				QualityStageResultJSON: qsr,
			})
		},
	}
}

func releaseCmd() *cli.Command {
	return &cli.Command{
		Name:  "release",
		Usage: "append the release step-summary (job table + release/packages/run links)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "release-version", Sources: cli.EnvVars("RELEASE_VERSION")},
			&cli.StringFlag{Name: "release-branch", Sources: cli.EnvVars("RELEASE_BRANCH")},
			&cli.StringFlag{Name: "release-commit", Sources: cli.EnvVars("RELEASE_COMMIT")},
			&cli.StringFlag{Name: "release-actor", Sources: cli.EnvVars("RELEASE_ACTOR")},
			&cli.StringFlag{Name: "run-url", Sources: cli.EnvVars("CI_RUN_URL")},
			&cli.StringFlag{Name: "create-release-result", Sources: cli.EnvVars("CREATE_RELEASE_RESULT")},
			&cli.StringFlag{Name: "prepare-stage-result-json", Sources: cli.EnvVars("PREPARE_STAGE_RESULT_JSON")},
			&cli.StringFlag{Name: "build-stage-result-json", Sources: cli.EnvVars("BUILD_STAGE_RESULT_JSON")},
			&cli.StringFlag{Name: "publish-stage-result-json", Sources: cli.EnvVars("PUBLISH_STAGE_RESULT_JSON")},
			&cli.StringFlag{Name: "server-url", Sources: cli.EnvVars("CI_SERVER_URL")},
			&cli.StringFlag{Name: "repository", Sources: cli.EnvVars("CI_REPO", "GITHUB_REPOSITORY")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
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
		},
	}
}
