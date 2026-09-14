//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git_test

import (
	"testing"

	adaptergit "github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/isolatedgit"
)

// TestRemoteBranchCommit_ReadsExactlyTheNamedBranchFromTheRemote is the native
// half of the pre-mutation identity check that CommitPush makes before it
// commits. ls-remote patterns match trailing path components, so asking for
// refs/heads/main also lists a branch named a/refs/heads/main, which sorts
// first; only the exact ref comparison keeps it out. A tag named main and a
// local main that has moved on are on other commits too, so only a read of
// exactly refs/heads/main on the remote answers with the published commit. An absent branch reports not found rather than an error or an empty
// match.
//
// No t.Parallel(): isolatedgit.NewRepo scrubs the environment via t.Setenv.
func TestRemoteBranchCommit_ReadsExactlyTheNamedBranchFromTheRemote(t *testing.T) {
	repo := isolatedgit.NewRepo(t)
	remote := repo.AddBareRemote()
	published := repo.HeadSHA()

	decoy := repo.AddCommit("chore: decoy branch")
	repo.Git("push", "-q", remote, decoy+":refs/heads/a/refs/heads/main")

	tagged := repo.AddCommit("chore: tag named main")
	repo.Git("push", "-q", remote, tagged+":refs/tags/main")

	repo.AddCommit("chore: local main moved, not published")

	adapter := &adaptergit.Repo{Dir: repo.Dir}

	sha, exists, err := adapter.RemoteBranchCommit(t.Context(), "origin", "main", runcontext.Credential{})
	if err != nil || !exists || sha != published {
		t.Fatalf("RemoteBranchCommit(main) = %q, %v, %v; want the published %s", sha, exists, err, published)
	}

	sha, exists, err = adapter.RemoteBranchCommit(t.Context(), "origin", "release/9.x", runcontext.Credential{})
	if err != nil || exists || sha != "" {
		t.Fatalf("RemoteBranchCommit(release/9.x) = %q, %v, %v; want not found", sha, exists, err)
	}
}
