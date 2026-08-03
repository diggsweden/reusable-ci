// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"context"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
)

// flagTempDir names the scratch-directory flag. Its value reaches the app
// layer as Input.TempDir; the app never reads the environment for it.
const flagTempDir = "temp-dir"

// pinGit wires the git adapter to appvalidate.PinGit. Open has to hand back
// the interface the use case declares rather than *git.Repo, because Go has
// no covariant returns. That indirection is the whole point of the port.
type pinGit struct{}

func (pinGit) Open(dir string) appvalidate.PinGitOps {
	return &git.Repo{Dir: dir}
}

func (pinGit) Clone(ctx context.Context, remote, dir string) error {
	_, err := git.New().Run(ctx, "clone", "--quiet", "--filter=blob:none", "--no-checkout", remote, dir)

	return err
}

func pinReachabilityCmd() *cli.Command {
	return &cli.Command{
		Name:  "pin-reachability",
		Usage: "reject forgejo-ci commit pins that are no longer reachable from main or any tag",
		Description: `Checks workflow files for forgejo-ci@<40-hex-sha> references and fails
when a pin is not reachable from the configured main branch and is not the
commit pointed to by any tag. This catches history-rewrite/orphaned-pin failures
before object GC turns them into confusing runtime failures.

EXAMPLE:
   reusable-ci validate pin-reachability --workflow .forgejo/workflows/release.yml`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagRoot, Value: ".", Usage: "repository root used to resolve relative workflow paths"},
			&cli.StringSliceFlag{Name: flagWorkflow, Usage: "workflow file to scan for forgejo-ci pins (repeatable; required)"},
			&cli.StringFlag{Name: "remote", Sources: cli.EnvVars("FORGEJO_CI_REMOTE"), Usage: "forgejo-ci git remote to clone when --repo-dir is unset (required unless --repo-dir is set; no org default)"},
			&cli.StringFlag{Name: "repo-dir", Sources: cli.EnvVars("FORGEJO_CI_DIR"), Usage: "local forgejo-ci clone to check instead of cloning --remote"},
			&cli.StringFlag{Name: "main", Value: "main", Sources: cli.EnvVars("FORGEJO_CI_MAIN"), Usage: "branch ref treated as current main"},
			&cli.StringFlag{Name: "subject", Required: true, Usage: "pin subject to scan before @<sha> (your reusable-workflow repo slug, e.g. forgejo-ci)"},
			&cli.StringFlag{Name: flagTempDir, Sources: cienv.TempDir(), Usage: "scratch directory for the temporary clone"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			workflows := cmd.StringSlice(flagWorkflow)

			root := cmd.String(flagRoot)
			for i, workflow := range workflows {
				if root != "" && !filepath.IsAbs(workflow) {
					workflows[i] = filepath.Join(root, workflow)
				}
			}

			return appvalidate.PinReachability(ctx, pinGit{}, os.Stderr, appvalidate.PinReachabilityInput{
				Workflows: workflows,
				Remote:    cmd.String("remote"),
				RepoDir:   cmd.String("repo-dir"),
				Main:      cmd.String("main"),
				Subject:   cmd.String("subject"),
				TempDir:   cmd.String(flagTempDir),
			})
		},
	}
}
