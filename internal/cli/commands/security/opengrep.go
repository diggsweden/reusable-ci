// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package security

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/opengrep"
	appsecurity "github.com/diggsweden/reusable-ci/internal/app/security"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
)

func runOpengrepCmd() *cli.Command {
	return &cli.Command{
		Name:  "run-opengrep",
		Usage: "run an opengrep SAST scan, emit findings + JSON/SARIF/text/GitLab-SAST artifacts, write step summary",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config", Sources: cli.EnvVars("OPENGREP_CONFIG")},
			&cli.StringFlag{Name: "fail-on-severity", Sources: cli.EnvVars("OPENGREP_FAIL_ON_SEVERITY")},
			&cli.StringFlag{Name: "target-path", Sources: cli.EnvVars("OPENGREP_TARGET_PATH")},
			&cli.StringFlag{Name: "json-file", Sources: cli.EnvVars("OPENGREP_JSON_FILE")},
			&cli.StringFlag{Name: "sarif-file", Sources: cli.EnvVars("OPENGREP_SARIF_FILE")},
			&cli.StringFlag{Name: "text-file", Sources: cli.EnvVars("OPENGREP_TEXT_FILE")},
			&cli.StringFlag{Name: "gitlab-sast-file", Sources: cli.EnvVars("OPENGREP_GITLAB_SAST_FILE")},
			&cli.BoolFlag{Name: "has-code-scanning-token", Sources: cli.EnvVars("HAS_CODE_SCANNING_TOKEN")},
			&cli.StringFlag{Name: "run-url", Sources: cli.EnvVars("CI_RUN_URL")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			annot := deps.Annotator(cmd)
			return appsecurity.RunOpengrep(ctx, opengrep.New(), d.OutputSink, d.SummarySink, os.Stdout, os.Stderr, annot, appsecurity.RunOpengrepInput{
				Config:             cmd.String("config"),
				FailOnSeverity:     cmd.String("fail-on-severity"),
				TargetPath:         cmd.String("target-path"),
				JSONFile:           cmd.String("json-file"),
				SARIFFile:          cmd.String("sarif-file"),
				TextFile:           cmd.String("text-file"),
				GitLabSASTFile:     cmd.String("gitlab-sast-file"),
				Platform:           d.Platform,
				HasCodeScanningTok: cmd.Bool("has-code-scanning-token") || os.Getenv("CODE_SCANNING_TOKEN") != "",
				RunURL:             cmd.String("run-url"),
			})
		},
	}
}
