// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appvalidate "github.com/diggsweden/reusable-ci/internal/app/validate"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
)

func changelogCmd() *cli.Command {
	return &cli.Command{
		Name:  "changelog",
		Usage: "verify a changelog file's presence (or read its content into the output sink)",
		Description: "Two modes — full (the file must exist; emits a line count) and " +
			"minimal (file may be absent; emits content=<file body> or content=" +
			"\"No changes for this release\" via the OutputSink).",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "path",
				Required: true,
				Usage:    "changelog file path",
			},
			&cli.BoolFlag{
				Name:  "required",
				Usage: "fail when the file is missing (full-changelog mode)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appvalidate.Changelog(ctx, d.OutputSink, os.Stderr, appvalidate.ChangelogInput{
					Path:     cmd.String("path"),
					Required: cmd.Bool("required"),
				})
			})
		},
	}
}
