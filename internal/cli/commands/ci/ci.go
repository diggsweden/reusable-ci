// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package ci wires `reusable-ci ci <subcmd>` for misc CI-platform
// helpers (debug listings, workspace inspection).
package ci

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appci "github.com/diggsweden/reusable-ci/internal/app/ci"
)

// New returns the `ci` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "ci",
		Usage: "CI-platform debug + introspection helpers",
		Commands: []*cli.Command{
			debugWorkspaceCmd(),
		},
	}
}

func debugWorkspaceCmd() *cli.Command {
	return &cli.Command{
		Name:  "debug-workspace",
		Usage: "print workspace listing + .github-shared listing + GitHub action context",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "action-repository", Sources: cli.EnvVars("ACTION_REPOSITORY")},
			&cli.StringFlag{Name: "action-ref", Sources: cli.EnvVars("ACTION_REF")},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return appci.DebugWorkspace(os.Stdout, appci.DebugWorkspaceInput{
				ActionRepository: cmd.String("action-repository"),
				ActionRef:        cmd.String("action-ref"),
			})
		},
	}
}
