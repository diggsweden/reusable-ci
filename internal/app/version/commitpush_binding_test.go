// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domaingit "github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

type bindingCommitRepo struct {
	fakeCommitPushRepo
	calls      []string
	failAt     string
	createdSHA string
	parents    []string
	remote     string
	branch     string
	readToken  runcontext.Credential
	pushToken  runcontext.Credential
}

func (f *bindingCommitRepo) HasStagedChanges(ctx context.Context) (bool, error) {
	if err := f.record("index"); err != nil {
		return false, err
	}

	return f.fakeCommitPushRepo.HasStagedChanges(ctx)
}

func (f *bindingCommitRepo) RevParse(_ context.Context, ref string) (string, error) {
	call := "base:" + ref

	sha := f.localSHA
	if f.committed {
		call, sha = "capture:"+ref, f.createdSHA
	}

	return sha, f.record(call)
}

func (f *bindingCommitRepo) CheckOriginPushDestination(_ context.Context) error {
	return f.record("destination")
}

func (f *bindingCommitRepo) RemoteBranchCommit(_ context.Context, remote, branch string, cred runcontext.Credential) (string, bool, error) {
	f.remote, f.branch, f.readToken = remote, branch, cred

	return f.remoteSHA, f.remoteSeen, f.record("remote")
}

func (f *bindingCommitRepo) AddPathspecs(ctx context.Context, paths []string) {
	f.calls = append(f.calls, "add")
	f.fakeCommitPushRepo.AddPathspecs(ctx, paths)
}

func (f *bindingCommitRepo) HasPathspecChanges(ctx context.Context, paths []string) (bool, error) {
	if err := f.record("status"); err != nil {
		return false, err
	}

	return f.fakeCommitPushRepo.HasPathspecChanges(ctx, paths)
}

func (f *bindingCommitRepo) Config(ctx context.Context, key, value string) error {
	if err := f.record(key); err != nil {
		return err
	}

	return f.fakeCommitPushRepo.Config(ctx, key, value)
}

func (f *bindingCommitRepo) Commit(ctx context.Context, in domaingit.CommitInput) error {
	if err := f.record("commit"); err != nil {
		return err
	}

	return f.fakeCommitPushRepo.Commit(ctx, in)
}

func (f *bindingCommitRepo) CommitParents(_ context.Context, sha string) ([]string, error) {
	// Moving HEAD after capture must not change the object being published.
	f.createdSHA = strings.Repeat("d", len(sha))

	return f.parents, f.record("parents:" + sha)
}

func (f *bindingCommitRepo) PushCommitWithLease(ctx context.Context, in domaingit.BranchPushInput, cred runcontext.Credential) error {
	f.pushToken = cred
	if err := f.record("lease"); err != nil {
		return err
	}

	return f.fakeCommitPushRepo.PushCommitWithLease(ctx, in, cred)
}

func (f *bindingCommitRepo) record(call string) error {
	f.calls = append(f.calls, call)
	if call == f.failAt {
		return errs.ErrPermissionDenied
	}

	return nil
}

func newBindingCommitRepo(size int) (*bindingCommitRepo, appversion.CommitPushInput) {
	base := strings.Repeat("a", size)

	return &bindingCommitRepo{
			fakeCommitPushRepo: fakeCommitPushRepo{localSHA: base, remoteSHA: base, remoteSeen: true, hasStaged: true},
			createdSHA:         strings.Repeat("c", size), parents: []string{base},
		}, appversion.CommitPushInput{Branch: "release/1.x", AuthorName: "Fixture", AuthorEmail: "fixture@example.invalid",
			Message: "release\n\nbody", FilePattern: "CHANGELOG.md package.json", ExpectedSHA: base}
}

func TestCommitBinding_CapturedObjectAndLease(t *testing.T) {
	t.Parallel()

	for _, size := range []int{40, 64} {
		repo, in := newBindingCommitRepo(size)
		in.Token = runcontext.OperatorCredential("fixture-token")
		created := repo.createdSHA
		require.NoError(t, appversion.CommitPush(t.Context(), repo, &bytes.Buffer{}, in))
		require.Equal(t, []string{"index", "base:HEAD", "destination", "remote", "add", "index", "user.name", "user.email", "commit", "capture:HEAD", "parents:" + created, "lease"}, repo.calls)
		require.Equal(t, domaingit.BranchPushInput{CommitSHA: created, Branch: in.Branch, ExpectedSHA: in.ExpectedSHA}, repo.leasedPush)
		require.Equal(t, "origin", repo.remote)
		require.Equal(t, in.Branch, repo.branch)
		require.Equal(t, in.Token, repo.readToken)
		require.Equal(t, in.Token, repo.pushToken)
		require.Equal(t, domaingit.CommitInput{Message: in.Message, AuthorName: in.AuthorName, AuthorEmail: in.AuthorEmail, Signoff: true, NoHooks: true}, repo.commit)
	}
}

func TestCommitBinding_PreflightRefusals(t *testing.T) {
	t.Parallel()

	for _, defect := range []string{"local", "remote", "missing", "uppercase", "malformed-sha", "branch"} {
		for _, dry := range []bool{false, true} {
			repo, in := newBindingCommitRepo(40)
			in.DryRun = dry
			want := errs.ErrValidation

			switch defect {
			case "local":
				repo.localSHA = strings.Repeat("b", 40)
			case "remote":
				repo.remoteSHA = strings.Repeat("b", 40)
			case "missing":
				repo.remoteSeen = false
			case "uppercase":
				repo.remoteSHA = strings.ToUpper(in.ExpectedSHA)
			case "malformed-sha":
				in.ExpectedSHA, want = "HEAD", errs.ErrUsage
			case "branch":
				in.Branch, want = "main:refs/tags/v1.2.3", errs.ErrUsage
			}

			var out bytes.Buffer
			require.ErrorIs(t, appversion.CommitPush(t.Context(), repo, &out, in), want, "%s dry=%v", defect, dry)
			require.Empty(t, repo.cfg)
			require.Empty(t, repo.added)
			require.False(t, repo.committed)
			require.Empty(t, repo.pushedRef)
			require.Empty(t, out.String())
		}
	}
}

func TestCommitBinding_FailuresStopAtBoundary(t *testing.T) {
	t.Parallel()

	repo, _ := newBindingCommitRepo(40)

	steps := []string{"index", "base:HEAD", "destination", "remote", "add", "index", "user.name", "user.email", "commit", "capture:HEAD", "parents:" + repo.createdSHA, "lease"}
	for i, step := range steps {
		if step == "add" || i == 5 {
			continue // AddPathspecs has no error return; the first index read is covered.
		}

		t.Run(step, func(t *testing.T) {
			t.Parallel()

			repo, in := newBindingCommitRepo(40)
			repo.failAt = step

			var out bytes.Buffer
			require.ErrorIs(t, appversion.CommitPush(t.Context(), repo, &out, in), errs.ErrPermissionDenied)
			require.Equal(t, steps[:i+1], repo.calls)
			require.Empty(t, repo.pushedRef)
			require.NotContains(t, out.String(), "Pushed")

			if i < 4 {
				require.Empty(t, repo.cfg)
				require.Empty(t, repo.added)
			}
		})
	}
}

func TestCommitBinding_RejectsUnboundCreatedCommit(t *testing.T) {
	t.Parallel()

	for _, defect := range []string{"old-oid", "symbolic", "root", "wrong-parent", "merge"} {
		repo, in := newBindingCommitRepo(40)

		switch defect {
		case "old-oid":
			repo.createdSHA = in.ExpectedSHA
		case "symbolic":
			repo.createdSHA = "HEAD"
		case "root":
			repo.parents = nil
		case "wrong-parent":
			repo.parents = []string{strings.Repeat("b", 40)}
		case "merge":
			repo.parents = append(repo.parents, strings.Repeat("b", 40))
		}

		require.ErrorIs(t, appversion.CommitPush(t.Context(), repo, &bytes.Buffer{}, in), errs.ErrValidation, defect)
		require.True(t, repo.committed)
		require.Empty(t, repo.pushedRef)
		require.NotContains(t, repo.calls, "lease")
	}
}

func TestCommitBinding_PreviewAndNoOp(t *testing.T) {
	t.Parallel()

	for _, dry := range []bool{false, true} {
		repo, in := newBindingCommitRepo(40)
		in.DryRun, repo.hasStaged = dry, dry
		require.NoError(t, appversion.CommitPush(t.Context(), repo, &bytes.Buffer{}, in))

		want := []string{"index", "base:HEAD", "destination", "remote"}
		if dry {
			want = append(want, "status")

			require.Empty(t, repo.added)
		} else {
			want = append(want, "add", "index")
		}

		require.Equal(t, want, repo.calls)
		require.Empty(t, repo.cfg)
		require.False(t, repo.committed)
		require.Empty(t, repo.pushedRef)
	}
}

func TestCommitBinding_EmptyExpectedSHAIsExplicitLegacyPush(t *testing.T) {
	t.Parallel()

	repo, in := newBindingCommitRepo(40)
	in.ExpectedSHA = ""
	require.NoError(t, appversion.CommitPush(t.Context(), repo, &bytes.Buffer{}, in))
	require.Equal(t, []string{"index", "add", "index", "user.name", "user.email", "commit"}, repo.calls)
	require.Equal(t, "HEAD", repo.pushedRef)
	require.Equal(t, in.Branch, repo.pushedDest)
	require.Equal(t, domaingit.BranchPushInput{}, repo.leasedPush)
}

// TestCommitPush_RequiredFieldErrorsTouchNothing refuses each missing field as
// a usage error naming it, against a recording fake that must see no call at
// all. It used to run against a real git adapter with an empty directory, so
// a regression in the refusal would have staged, committed and pushed in the
// test process's own checkout.
func TestCommitPush_RequiredFieldErrorsTouchNothing(t *testing.T) {
	t.Parallel()

	tests := map[string]appversion.CommitPushInput{
		"BRANCH":              {AuthorName: "n", AuthorEmail: "e", Message: "m", FilePattern: "x"},
		"COMMIT_AUTHOR_NAME":  {Branch: "main", AuthorEmail: "e", Message: "m", FilePattern: "x"},
		"COMMIT_AUTHOR_EMAIL": {Branch: "main", AuthorName: "n", Message: "m", FilePattern: "x"},
		"COMMIT_MESSAGE":      {Branch: "main", AuthorName: "n", AuthorEmail: "e", FilePattern: "x"},
		"FILE_PATTERN":        {Branch: "main", AuthorName: "n", AuthorEmail: "e", Message: "m"},
	}

	for missing, in := range tests {
		t.Run("missing-"+missing, func(t *testing.T) {
			t.Parallel()

			repo, _ := newBindingCommitRepo(40)

			var out bytes.Buffer

			err := appversion.CommitPush(t.Context(), repo, &out, in)
			require.ErrorIs(t, err, errs.ErrUsage)
			require.ErrorContains(t, err, missing)
			require.Empty(t, repo.calls)
			require.Empty(t, repo.cfg)
			require.Empty(t, repo.added)
			require.False(t, repo.committed)
			require.Empty(t, repo.pushedRef)
			require.Empty(t, out.String())
		})
	}
}
