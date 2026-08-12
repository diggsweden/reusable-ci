// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build live

package conformance_test

// PAR-CHK-1: `platform checkout` produces a working tree at the requested ref,
// on every forge.
//
// The last verb with a provider role and no live coverage. It is unlike every
// other scenario here in that its result is a directory rather than an API
// state, and that is exactly why it needs a real forge: it composes a clone URL,
// decides whether to put a credential in it, asks the forge for the repository's
// object format, and hands all of it to git. Each of those is a per-forge
// decision, and a fake can only confirm the shape we already assumed.
//
// `--object-format` is deliberately not passed. Omitting it is what routes the
// command through FetchRepoMetadata — the role under test — because an explicit
// value short-circuits the lookup entirely. So a forge whose metadata call is
// broken fails here rather than being quietly skipped.
//
// The assertions are made with git against the resulting tree, not by trusting
// the command's exit code: a checkout that exits 0 having produced an empty
// directory is the failure worth catching, and it is invisible from the API side.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

// afterTagFile is committed after the tag, so its absence proves the checkout
// honoured the ref instead of taking whatever the branch points at now.
const afterTagFile = "after-tag.txt"

func TestCheckout_ProducesAWorkingTreeAtTheRequestedRef(t *testing.T) {
	const tag = "v0.0.1-checkout"

	for _, forge := range forgesClaiming(t, alwaysValidatesTokens, "repository metadata") {
		t.Run(string(forge), func(t *testing.T) {
			target := livetest.Accept(t, forge)
			repo := livetest.NewScratchRepo(t, target, "checkout")

			livetest.PrepareTag(t, target, repo, tag)

			// Move the default branch past the tag. Without this the tag and the
			// branch head are the same commit, and a checkout that ignored --ref
			// entirely would land on that commit and pass every assertion below.
			livetest.CommitFile(t, target, repo, afterTagFile,
				"livetest: advance main past "+tag, "committed after the tag\n")

			workspace := filepath.Join(t.TempDir(), "tree")

			run := livetest.CLI(t, target, repo,
				"platform", "checkout",
				"--repository", livetest.RepoSlug(target, repo),
				"--server-url", target.BaseURL(),
				"--ref", tag,
				"--workspace", workspace,
			)
			if run.ExitCode != 0 {
				t.Fatalf("%s: checkout failed (exit %d)\nstdout: %s\nstderr: %s",
					forge, run.ExitCode, run.Stdout, run.Stderr)
			}

			// A directory is not a checkout. This is the assertion the exit code
			// cannot make.
			if _, err := os.Stat(filepath.Join(workspace, ".git")); err != nil {
				t.Fatalf("%s: checkout exited 0 but left no git repository at %s: %v", forge, workspace, err)
			}

			// Both forges' scratch repositories carry a README at this point —
			// Forgejo from auto_init, GitLab from the commit that creates its
			// default branch — so a tracked file is expected, and an empty tree
			// means the fetch produced nothing.
			if _, err := os.Stat(filepath.Join(workspace, "README.md")); err != nil {
				t.Errorf("%s: the working tree has no README.md, so the checkout produced an empty tree: %v", forge, err)
			}

			head := git(t, workspace, "rev-parse", "HEAD")
			if head == "" {
				t.Fatalf("%s: HEAD does not resolve in the checked-out tree", forge)
			}

			// The ref is the claim, not merely that something was cloned, and the
			// forge is asked which commit the tag points at rather than the clone.
			// Asking the clone would be asking git about what git just did, and a
			// clone made at a branch need not carry the tag at all.
			if want := livetest.TagCommitSHA(t, target, repo, tag); want != head {
				t.Errorf("%s: HEAD is %s but the forge says %s is %s — the checkout did not land on the requested ref",
					forge, head, tag, want)
			}

			// The same claim from the other side, and the one that cannot be
			// satisfied by accident: a file committed after the tag must not be
			// in a tree checked out at the tag.
			if _, err := os.Stat(filepath.Join(workspace, afterTagFile)); err == nil {
				t.Errorf("%s: %s is present, so the checkout took the branch head rather than %s",
					forge, afterTagFile, tag)
			}
		})
	}
}

// git runs one read-only git command in dir and returns its trimmed stdout.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // fixed read-only git subcommands from the scenario, not from input.

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s in %s: %v", strings.Join(args, " "), dir, err)
	}

	return strings.TrimSpace(string(out))
}
