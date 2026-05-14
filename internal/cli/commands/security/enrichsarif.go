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

func enrichGitHubSARIFCmd() *cli.Command {
	return &cli.Command{
		Name:  "enrich-github-sarif",
		Usage: "populate partialFingerprints.primaryLocationLineHash on every SARIF result for GitHub Code Scanning dedupe",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "sarif-file", Sources: cli.EnvVars("SARIF_FILE")},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			annot := deps.Annotator(cmd)
			return appsecurity.EnrichGitHubSARIFFile(os.Stdout, os.Stderr, annot, appsecurity.EnrichGitHubSARIFInput{
				Path: cmd.String("sarif-file"),
			})
		},
	}
}
