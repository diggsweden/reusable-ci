// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/github"
	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

// publishSelfRuntimeCLICmd is hidden because it is an implementation detail of
// this repository's self-runtime workflow, not a consumer release primitive.
func publishSelfRuntimeCLICmd() *cli.Command {
	return &cli.Command{
		Name:   "publish-self-runtime-cli",
		Usage:  "replace diggsweden/reusable-ci's mutable " + domainrelease.SelfRuntimeChannelTag + " CLI release at an exact commit",
		Hidden: true,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagRepository, Required: true, Sources: cienv.Repository(), Usage: "must be diggsweden/reusable-ci"},
			&cli.StringFlag{Name: "source-ref", Required: true, Usage: "trusted refs/heads/* branch that must still resolve to --commit"},
			&cli.StringFlag{Name: "commit", Required: true, Sources: cienv.Commit(), Usage: "full commit SHA the recreated tag must resolve to"},
			&cli.StringFlag{Name: "assets-dir", Required: true, Usage: "directory containing the exact signed build-cli GoReleaser asset set"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return apprelease.PublishSelfRuntimeCLI(ctx, github.New(), os.Stderr, apprelease.PublishSelfRuntimeCLIInput{
				Repository: cmd.String(flagRepository),
				SourceRef:  cmd.String("source-ref"),
				TargetSHA:  cmd.String("commit"),
				AssetsDir:  cmd.String("assets-dir"),
			})
		},
	}
}
