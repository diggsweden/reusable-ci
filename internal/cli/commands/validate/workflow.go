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

// workflowGroup wires `reusable-ci validate workflow <verb>` — every
// subcommand here scans the repository's reusable workflow YAML for a
// structural rule (literal-default inputs, removed v3 contracts).
func workflowGroup() *cli.Command {
	return &cli.Command{
		Name:  "workflow",
		Usage: "scan reusable workflow YAML for structural rules",
		Commands: []*cli.Command{
			workflowInputDefaultsCmd(),
			workflowContractResidueCmd(),
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
			return appvalidate.WorkflowInputDefaults(os.Stderr, deps.Annotator(cmd), appvalidate.WorkflowInputDefaultsInput{
				Root: cmd.String("root"),
			})
		},
	}
}

func workflowContractResidueCmd() *cli.Command {
	return &cli.Command{
		Name:  "contract-residue",
		Usage: "reject removed v3-incompatible output contracts and aliases",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "root", Value: ".", Usage: "repository root to scan"},
			&cli.StringSliceFlag{Name: "path", Usage: "path under root to scan (repeatable); defaults to source/workflow/doc roots"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return appvalidate.ContractResidue(os.Stderr, deps.Annotator(cmd), appvalidate.ContractResidueInput{
				Root:  cmd.String("root"),
				Paths: cmd.StringSlice("path"),
			})
		},
	}
}
