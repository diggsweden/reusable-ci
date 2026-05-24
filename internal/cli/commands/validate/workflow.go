// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appvalidate "github.com/diggsweden/reusable-ci/internal/app/validate"
)

// workflowGroup wires `reusable-ci validate workflow <verb>` — every
// subcommand here scans the repository's reusable workflow YAML for a
// structural rule (literal-default inputs, removed v3 contracts).
func workflowGroup() *cli.Command {
	return &cli.Command{
		Name:  "workflow",
		Usage: "scan reusable workflow YAML for structural rules",
		Commands: []*cli.Command{
			workflowInputDefaultsCmd(),
			workflowV3ContractsCmd(),
		},
	}
}

func workflowInputDefaultsCmd() *cli.Command {
	return &cli.Command{
		Name:  "input-defaults",
		Usage: "verify reusable workflow_call input defaults are literal values, not expressions",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "root", Value: ".", Usage: "repository root containing .github/workflows"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return appvalidate.WorkflowInputDefaults(os.Stderr, appvalidate.WorkflowInputDefaultsInput{
				Root: cmd.String("root"),
			})
		},
	}
}

func workflowV3ContractsCmd() *cli.Command {
	return &cli.Command{
		Name:  "v3-contracts",
		Usage: "reject removed v3-incompatible output contracts and aliases",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "root", Value: ".", Usage: "repository root to scan"},
			&cli.StringSliceFlag{Name: "path", Usage: "path under root to scan (repeatable); defaults to source/workflow/doc roots"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return appvalidate.V3Contracts(os.Stderr, appvalidate.V3ContractsInput{
				Root:  cmd.String("root"),
				Paths: cmd.StringSlice("path"),
			})
		},
	}
}
