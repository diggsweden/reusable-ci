// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package platform

import (
	"context"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	appci "github.com/diggsweden/reusable-ci/v3/internal/app/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

func checkoutCmd() *cli.Command {
	return &cli.Command{
		Name:  "checkout",
		Usage: "exact, credential-free, sha256-aware checkout of a repository into the workspace",
		Description: "Git-based checkout that works in minimal containers and for sha256 repositories. " +
			"Resolves the object format from the forge, " +
			"materializes the workspace at the given ref, and writes the resolved commit SHA to the output sink. " +
			"The token (when set) is sent as transient HTTP Basic auth and never persisted to .git/config.\n\n" +
			"EXAMPLES:\n" +
			"   # Check out a tag of a repo into the current directory\n" +
			"   reusable-ci platform checkout --repository org/app --ref v1.2.3\n\n" +
			"   # Shallow checkout into a subdirectory\n" +
			"   reusable-ci platform checkout --repository org/app --ref main --depth 1 --path app",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "repository", Sources: cienv.Repository(), Usage: "owner/name to check out"},
			&cli.StringFlag{Name: "server-url", Sources: cienv.ServerURL(), Usage: "forge base URL (e.g. https://codeberg.org)"},
			&cli.StringFlag{Name: "ref", Sources: cienv.CheckoutRef(), Usage: "commit SHA, refs/tags/…, refs/heads/…, or a bare tag/branch name to check out. Set via $CHECKOUT_REF; defaults to the triggering commit."},
			&cli.StringFlag{Name: "workspace", Sources: cienv.Workspace(), Usage: "target directory (default: current directory)"},
			&cli.StringFlag{Name: "token", Sources: cienv.Token(), Usage: "clone token; empty for an anonymous checkout"},
			&cli.StringFlag{Name: "object-format", Sources: cli.EnvVars("CHECKOUT_OBJECT_FORMAT"), Usage: "override the forge-reported object format (sha1|sha256); skips the metadata lookup"},
			&cli.StringFlag{Name: "fetch-base", Sources: cli.EnvVars("CHECKOUT_FETCH_BASE"), Usage: "extra branch to also fetch (diff/commit-range checks)"},
			&cli.BoolFlag{Name: "fetch-tags", Sources: cli.EnvVars("CHECKOUT_FETCH_TAGS"), Usage: "also fetch all tags (the JS actions/checkout fetch-tags:true; needed for git-cliff/changelog)"},
			&cli.BoolFlag{Name: "fetch-all-refs", Sources: cli.EnvVars("CHECKOUT_FETCH_ALL_REFS"), Usage: "fetch every branch into refs/remotes/origin/* plus all tags (the JS actions/checkout fetch-depth:0); needed by builds that read git topology, e.g. `git rev-list --count origin/main` or `git describe`"},                                                  //nolint:lll // single-line flag declaration for grep-ability, matching the package convention.
			&cli.IntFlag{Name: "depth", Sources: cli.EnvVars("CHECKOUT_FETCH_DEPTH"), Usage: "shallow history depth for the checked-out ref (git --depth); 0 = full history (the JS actions/checkout fetch-depth, where 0 means all). Use 1 to speed up build/scan jobs that only need the tree. Orthogonal to --fetch-all-refs, which always brings full topology."}, //nolint:lll // single-line flag declaration for grep-ability, matching the package convention.
			&cli.StringSliceFlag{Name: "sparse", Sources: cli.EnvVars("CHECKOUT_SPARSE"), Usage: "cone-mode sparse-checkout dir(s), e.g. scripts/bootstrap; restricts the working tree to these subtrees (repeatable / comma-separated)"},
			&cli.StringFlag{Name: "path", Sources: cli.EnvVars("CHECKOUT_PATH"), Usage: "subdirectory to check out into, relative to $GITHUB_WORKSPACE (the JS actions/checkout path:); alternative to --workspace"},
			&cli.StringFlag{Name: "output-key", Value: "checkout-sha", Sources: cli.EnvVars("OUTPUT_KEY"), Usage: "key written to the output sink for the resolved SHA"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				workspace, err := resolveWorkspace(cmd.String("workspace"), cmd.String("path"))
				if err != nil {
					return err
				}

				format, err := resolveObjectFormat(ctx, dep, cmd)
				if err != nil {
					return err
				}

				repo := &git.Repo{Dir: workspace}

				_, err = appci.Checkout(ctx, repo, dep.OutputSink, os.Stderr, appci.CheckoutInput{
					Repository:   cmd.String("repository"),
					ServerURL:    cmd.String("server-url"),
					Ref:          cmd.String("ref"),
					Workspace:    workspace,
					Token:        cmd.String("token"),
					ObjectFormat: format,
					FetchBase:    cmd.String("fetch-base"),
					FetchTags:    cmd.Bool("fetch-tags"),
					FetchAllRefs: cmd.Bool("fetch-all-refs"),
					Depth:        cmd.Int("depth"),
					Sparse:       cmd.StringSlice("sparse"),
					OutputKey:    cmd.String("output-key"),
				})

				return err
			})
		},
	}
}

// resolveWorkspace picks the checkout target directory. --path (the JS
// actions/checkout path:) wins and is resolved relative to the runner
// workspace — Forgejo's native $FORGEJO_WORKSPACE preferred over the
// $GITHUB_WORKSPACE alias (or cwd when neither is set), kept absolute so the
// app's safe.directory guard stays correct. Otherwise an explicit
// --workspace, else the current directory.
func resolveWorkspace(workspace, path string) (string, error) {
	if path != "" {
		base := runcontext.Workspace().Resolve(os.Getenv)
		if base == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return "", err
			}

			base = cwd
		}

		return filepath.Join(base, path), nil
	}

	if workspace != "" {
		return workspace, nil
	}

	return os.Getwd()
}

// resolveObjectFormat applies the override-or-forge policy: an explicit
// --object-format wins (and skips the network entirely); otherwise the
// provider's repo metadata supplies it. An empty result is fine — the app
// defaults it to sha1.
func resolveObjectFormat(ctx context.Context, dep *deps.Deps, cmd *cli.Command) (string, error) {
	if override := cmd.String("object-format"); override != "" {
		return override, nil
	}

	repo := cmd.String("repository")
	if repo == "" {
		return "", nil
	}

	md, err := dep.RepoMetadataFetcher().FetchRepoMetadata(ctx, repo)
	if err != nil {
		return "", err
	}

	return md.ObjectFormat, nil
}
