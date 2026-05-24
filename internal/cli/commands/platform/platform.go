// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package platform wires `reusable-ci platform <subcmd>` — helpers
// that introspect the CI runtime (GitHub Actions / GitLab CI / local)
// the binary is executing inside.
package platform

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/git"
	appci "github.com/diggsweden/reusable-ci/internal/app/ci"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
)

// New returns the `platform` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "platform",
		Usage: "introspect the CI runtime (debug workspace, resolve refs)",
		Commands: []*cli.Command{
			debugWorkspaceCmd(),
			resolveRefCmd(),
		},
	}
}

func resolveRefCmd() *cli.Command {
	return &cli.Command{
		Name:  "resolve-ref",
		Usage: "resolve a remote git ref to a commit SHA output",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "remote-url", Value: "https://github.com/diggsweden/reusable-ci", Sources: cli.EnvVars("REMOTE_URL"), Usage: "remote git URL queried with 'git ls-remote'"},
			&cli.StringFlag{Name: "ref", Sources: cli.EnvVars("REF"), Usage: "ref to resolve (tag, branch, or full refs/X/Y)"},
			&cli.StringFlag{Name: "output-key", Value: "sha", Sources: cli.EnvVars("OUTPUT_KEY"), Usage: "key written to the platform output sink"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				_, err := appci.ResolveRef(ctx, git.New(), d.OutputSink, os.Stderr, appci.ResolveRefInput{
					RemoteURL: cmd.String("remote-url"),
					Ref:       cmd.String("ref"),
					OutputKey: cmd.String("output-key"),
				})

				return err
			})
		},
	}
}

func debugWorkspaceCmd() *cli.Command {
	return &cli.Command{
		Name:  "debug-workspace",
		Usage: "print workspace listing + .github-shared listing + GitHub action context",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "action-repository", Sources: cli.EnvVars("ACTION_REPOSITORY"), Usage: "the calling action's repository (printed in the debug block)"},
			&cli.StringFlag{Name: "action-ref", Sources: cli.EnvVars("ACTION_REF"), Usage: "the calling action's ref (printed in the debug block)"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return appci.DebugWorkspace(os.Stderr, appci.DebugWorkspaceInput{
				ActionRepository: cmd.String("action-repository"),
				ActionRef:        cmd.String("action-ref"),
			})
		},
	}
}
