// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package security

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/internal/adapters/trivy"
	appsecurity "github.com/diggsweden/reusable-ci/internal/app/security"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/domain/security"
)

func scanDependenciesCmd() *cli.Command {
	return &cli.Command{
		Name:  "scan-dependencies",
		Usage: "scan project dependencies for known vulnerabilities (Trivy, diff-mode against base ref)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "fail-on-severity", Value: "critical", Sources: cli.EnvVars("FAIL_ON_SEVERITY")},
			&cli.StringFlag{Name: "scan-mode", Value: "diff", Sources: cli.EnvVars("SCAN_MODE")},
			&cli.StringFlag{Name: "scan-path", Value: ".", Sources: cli.EnvVars("SCAN_PATH")},
			&cli.StringFlag{Name: "base-ref", Sources: cli.EnvVars("CI_PR_BASE_REF")},
			&cli.StringFlag{Name: "sarif-file", Value: security.DefaultTrivySARIFFile},
			&cli.StringFlag{Name: "gitlab-dep-file", Value: security.DefaultTrivyGitLabDepFile},
			&cli.StringFlag{Name: "trivy-version", Sources: cli.EnvVars("TRIVY_VERSION")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			annot := deps.Annotator(cmd)
			return appsecurity.ScanDependencies(ctx, trivy.New(), git.New(), d.SummarySink, os.Stdout, os.Stderr, annot, appsecurity.ScanDependenciesInput{
				FailOnSeverity: cmd.String("fail-on-severity"),
				ScanMode:       security.ScanMode(cmd.String("scan-mode")),
				ScanPath:       cmd.String("scan-path"),
				BaseRef:        cmd.String("base-ref"),
				SARIFFile:      cmd.String("sarif-file"),
				GitLabDepFile:  cmd.String("gitlab-dep-file"),
				TrivyVersion:   cmd.String("trivy-version"),
			})
		},
	}
}
