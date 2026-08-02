// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
)

// jobGraphCmd wires `reusable-ci validate job-graph` — a static gate against a
// reusable-call job reading a skippable producer's outputs, which the forge
// aborts (and masks) at run time.
func jobGraphCmd() *cli.Command {
	return &cli.Command{
		Name:  "job-graph",
		Usage: "reject reusable-call jobs that read a skippable producer's outputs (masks the real failure)",
		Description: `A reusable-workflow-call job whose if/with reads needs.<P>.outputs where <P>
is skippable (a non-always() if) makes the forge abort the whole run with a
misleading "<P> is missing the output ..." that hides the real upstream failure.
Make such producers run unconditionally (if: always()) and always emit the
output, or annotate the consumer with '# job-graph-guard: allow reason=...'.

EXAMPLE:
   reusable-ci validate job-graph --root .
   reusable-ci validate job-graph --workflow .forgejo/workflows/release.yml`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagRoot, Value: ".", Usage: "repository root containing .github/workflows"},
			&cli.StringFlag{Name: "workflows-dir", Usage: "workflow directory to scan (defaults to <root>/.github/workflows)"},
			&cli.StringSliceFlag{Name: flagWorkflow, Usage: "exact workflow file to check instead of scanning the workflow directory (repeatable)"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return appvalidate.JobGraph(os.Stderr, deps.Annotator(cmd), appvalidate.JobGraphInput{
				Root:         cmd.String(flagRoot),
				WorkflowsDir: cmd.String("workflows-dir"),
				Workflows:    cmd.StringSlice(flagWorkflow),
			})
		},
	}
}
