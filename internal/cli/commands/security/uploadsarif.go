// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package security

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appsecurity "github.com/diggsweden/reusable-ci/internal/app/security"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
)

func uploadSARIFCmd() *cli.Command {
	return &cli.Command{
		Name:  "upload-sarif",
		Usage: "upload a SARIF file to the platform's code-scanning surface (GitHub Code Scanning; skipped on GitLab/local)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "sarif-file", Sources: cli.EnvVars("SARIF_FILE")},
			&cli.StringFlag{Name: "token", Sources: cli.EnvVars("CODE_SCANNING_TOKEN")},
			&cli.StringFlag{Name: "repository", Sources: cli.EnvVars("GITHUB_REPOSITORY")},
			&cli.StringFlag{Name: "sha", Sources: cli.EnvVars("GITHUB_SHA")},
			&cli.StringFlag{Name: "ref", Sources: cli.EnvVars("GITHUB_REF")},
			&cli.StringFlag{Name: "category", Sources: cli.EnvVars("SARIF_CATEGORY")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			annot := deps.Annotator(cmd)
			return appsecurity.UploadSARIF(ctx, d.Provider, os.Stdout, annot, appsecurity.UploadSARIFInput{
				SARIFFile:  cmd.String("sarif-file"),
				Token:      cmd.String("token"),
				Repository: cmd.String("repository"),
				SHA:        cmd.String("sha"),
				Ref:        cmd.String("ref"),
				Category:   cmd.String("category"),
			})
		},
	}
}
