// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appplan "github.com/diggsweden/reusable-ci/v3/internal/app/plan"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
)

// filePatternCmd wires `reusable-ci version file-pattern` — print the git
// pathspec the version-bump commit stages for a project type (or a verbatim
// override). It lives under `version` because it feeds `version commit-push`;
// the per-ecosystem pathspec logic stays in app/plan.
func filePatternCmd() *cli.Command {
	return &cli.Command{
		Name:  "file-pattern",
		Usage: "print the git pathspec the version-bump commit stages for a project type",
		Description: `EXAMPLE:
   reusable-ci version file-pattern --project-type maven`,
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
