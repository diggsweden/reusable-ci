// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package platform wires `reusable-ci platform <subcmd>` — git + workspace
// operations against the CI runtime (GitHub Actions / GitLab CI / local) the
// binary is executing inside: check out a repository, resolve a ref to a SHA,
// and inspect the workspace.
package platform

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	appci "github.com/diggsweden/reusable-ci/v3/internal/app/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// New returns the `platform` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "platform",
		Usage: "CI-runtime git + workspace operations (check out a repo, resolve a ref, inspect the workspace)",
		Commands: []*cli.Command{
			debugWorkspaceCmd(),
			resolveRefCmd(),
			checkoutCmd(),
		},
	}
}

func resolveRefCmd() *cli.Command {
	return &cli.Command{
		Name:  "resolve-ref",
		Usage: "resolve a remote git ref to a commit SHA output",
		Description: `EXAMPLE:
   reusable-ci platform resolve-ref --remote-url https://github.com/org/app --ref v1.2.3`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "remote-url", Sources: cli.EnvVars("REMOTE_URL"), Usage: "remote git URL queried with 'git ls-remote' (default: this repository, derived from --server-url + --repository)"},
			&cli.StringFlag{Name: "server-url", Sources: cienv.ServerURL(), Usage: "forge base URL used to derive --remote-url when it is unset"},
			&cli.StringFlag{Name: "repository", Sources: cienv.Repository(), Usage: `"owner/repo" used to derive --remote-url when it is unset`},
			&cli.StringFlag{Name: "ref", Sources: cienv.Ref(), Usage: "ref to resolve (tag, branch, or full refs/X/Y)"},
			&cli.StringFlag{Name: "output-key", Value: "sha", Sources: cli.EnvVars("OUTPUT_KEY"), Usage: "key written to the platform output sink"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			remoteURL, err := resolveRemoteURL(cmd.String("remote-url"), cmd.String("server-url"), cmd.String("repository"))
			if err != nil {
				return err
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				_, err := appci.ResolveRef(ctx, git.New(), d.OutputSink, os.Stderr, appci.ResolveRefInput{
					RemoteURL: remoteURL,
					Ref:       cmd.String("ref"),
					OutputKey: cmd.String("output-key"),
				})

				return err
			})
		},
	}
}

// resolveRemoteURL returns the explicit --remote-url, or derives it from the
// run context (server-url + repository) so `resolve-ref` targets the current
// repository on any forge without a baked-in org default.
func resolveRemoteURL(remoteURL, serverURL, repository string) (string, error) {
	if u := strings.TrimSpace(remoteURL); u != "" {
		return u, nil
	}

	server := strings.TrimRight(strings.TrimSpace(serverURL), "/")
	repo := strings.Trim(strings.TrimSpace(repository), "/")

	if server != "" && repo != "" {
		return server + "/" + repo, nil
	}

	return "", fmt.Errorf("platform resolve-ref: --remote-url is required (or provide --server-url and --repository): %w", errs.ErrUsage)
}

func debugWorkspaceCmd() *cli.Command {
	return &cli.Command{
		Name:  "debug-workspace",
		Usage: "print workspace listing + .github-shared listing + GitHub action context",
		Description: `EXAMPLE:
   # Print the workspace + action context for debugging (takes no required flags)
   reusable-ci platform debug-workspace`,
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
