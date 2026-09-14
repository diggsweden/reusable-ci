//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git_test

import (
	"context"
	"strings"
	"testing"

	adaptergit "github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	domaingit "github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/isolatedgit"
)

// The app layer checks origin/<branch> before it commits, but that check and
// the push are two separate moments. Everything between them is the lease's
// job, and the lease is an argv string: --force-with-lease=<ref>:<oid>. Builder
// tests pin the spelling; only real Git decides whether the destination
// actually honours it.
//
// The distinction that matters is narrow and easy to get backwards. A push
// carrying --force-with-lease is still a FORCE push. If the lease OID is wrong,
// or the flag names a ref the push does not update, the "safety" flag becomes
// an overwrite of whatever the other writer just published.
//
// No t.Parallel(): isolatedgit.NewRepo scrubs the environment via t.Setenv.

func TestPushCommitWithLease_RefusesWhenTheDestinationMovedAfterTheCheck(t *testing.T) {
	repo := isolatedgit.NewRepo(t)
	remote := repo.AddBareRemote()
	adapter := &adaptergit.Repo{Dir: repo.Dir}
	ctx := context.Background()

	// The OID the release run authorized itself against.
	authorized := repo.HeadSHA()
	created := repo.AddCommit("chore: release v1.2.3")

	// Another writer publishes between the check and the push.
	other := isolatedgit.NewRepo(t)
	other.Git("remote", "add", "shared", remote)
	other.Git("fetch", "-q", "shared", "main")
	other.Git("checkout", "-q", "-B", "main", "FETCH_HEAD")

	interloper := other.AddCommit("chore: someone else got there first")
	other.Git("push", "-q", "shared", "main")

	err := adapter.PushCommitWithLease(ctx, domaingit.BranchPushInput{
		CommitSHA: created, Branch: "main", ExpectedSHA: authorized,
	}, runcontext.Credential{})
	if err == nil {
		t.Fatal("a stale lease must not publish; --force-with-lease is still a force push when the lease is wrong")
	}

	if got := remoteRef(t, repo, remote, "refs/heads/main"); got != interloper {
		t.Errorf("origin/main = %s, want the other writer's commit %s (the lease failed to protect it)", got, interloper)
	}
}

// The same call with a current lease must succeed, or the refusal above would
// pass just as well against a function that can never push at all.
func TestPushCommitWithLease_PublishesTheCapturedCommitWhenTheLeaseHolds(t *testing.T) {
	repo := isolatedgit.NewRepo(t)
	remote := repo.AddBareRemote()
	adapter := &adaptergit.Repo{Dir: repo.Dir}
	ctx := context.Background()

	authorized := repo.HeadSHA()

	created := repo.AddCommit("chore: release v1.2.3")
	// A later local commit the run did not capture. The push names an OID, not
	// a branch tip, so this must stay behind.
	repo.AddCommit("wip: not part of the release")

	if err := adapter.PushCommitWithLease(ctx, domaingit.BranchPushInput{
		CommitSHA: created, Branch: "main", ExpectedSHA: authorized,
	}, runcontext.Credential{}); err != nil {
		t.Fatalf("PushCommitWithLease: %v", err)
	}

	if got := remoteRef(t, repo, remote, "refs/heads/main"); got != created {
		t.Errorf("origin/main = %s, want the captured commit %s", got, created)
	}
}

// remoteRef resolves a ref inside the bare remote through Git, so the assertion
// reads the destination and is not fooled by loose-versus-packed storage.
func remoteRef(t *testing.T, repo *isolatedgit.Repo, remote, ref string) string {
	t.Helper()

	row := repo.Git("ls-remote", "--", remote, ref)
	if row == "" {
		t.Fatalf("ref %s is absent from the bare remote", ref)
	}

	sha, name, found := strings.Cut(row, "\t")
	if !found || strings.TrimSpace(name) != ref {
		t.Fatalf("unparsable ls-remote row %q", row)
	}

	return strings.TrimSpace(sha)
}
