//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/isolatedgit"
)

// remoteMain reads refs/heads/main straight from the bare remote.
func remoteMain(t *testing.T, remote string) string {
	t.Helper()

	//nolint:gosec // test infra; remote is the test's own bare repository.
	out, err := exec.CommandContext(t.Context(), "git", "--git-dir", remote, "rev-parse", "refs/heads/main").CombinedOutput()
	require.NoError(t, err, string(out))

	return strings.TrimSpace(string(out))
}

// changedPaths lists the paths the commit changed relative to its parent.
func changedPaths(r *isolatedgit.Repo, commit string) []string {
	return strings.Fields(r.Git("diff-tree", "--no-commit-id", "--name-only", "-r", commit))
}

func commitPushFixture(t *testing.T) (*isolatedgit.Repo, string) {
	t.Helper()

	r := isolatedgit.NewRepo(t)
	remote := r.AddBareRemote()

	require.NoError(t, os.WriteFile(filepath.Join(r.Dir, "CHANGELOG.md"), []byte("# v1\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(r.Dir, "unrelated.txt"), []byte("not requested\n"), 0o600))

	return r, remote
}

// TestCommitPush_PublishesExactlyTheRequestedPathsToTheRemote proves the
// publication against a bare remote rather than the local log: for the legacy
// and the leased mode the remote branch moves to the new local commit, whose
// only parent is the previous head and whose only changed path is the
// requested one, while an unrelated new file stays untracked.
func TestCommitPush_PublishesExactlyTheRequestedPathsToTheRemote(t *testing.T) {
	for _, leased := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "leased"}[leased], func(t *testing.T) {
			r, remote := commitPushFixture(t)
			before := r.HeadSHA()

			in := appversion.CommitPushInput{Branch: "main", AuthorName: "Test Bot", AuthorEmail: "bot@example.invalid", Message: "chore(release): v1.0.0", FilePattern: "CHANGELOG.md"}
			if leased {
				in.ExpectedSHA = before
			}

			require.NoError(t, appversion.CommitPush(t.Context(), &git.Repo{Dir: r.Dir}, &bytes.Buffer{}, in))

			after := r.HeadSHA()
			require.NotEqual(t, before, after)
			require.Equal(t, after, remoteMain(t, remote))
			require.Equal(t, before, r.Git("rev-parse", after+"^"))
			require.Equal(t, []string{"CHANGELOG.md"}, changedPaths(r, after))
			require.Equal(t, "?? unrelated.txt", r.Git("status", "--porcelain", "--", "unrelated.txt"))
		})
	}
}

// TestCommitPush_RemoteUnchangedOnNoOpAndRefusal covers the two outcomes that
// must leave the remote alone: nothing to commit (neither the local nor the
// remote head moves) and a leased request whose authorized source is no longer
// the remote head because another writer pushed (refused before staging or
// committing, with the other writer's commit still on the remote).
func TestCommitPush_RemoteUnchangedOnNoOpAndRefusal(t *testing.T) {
	t.Run("nothing to commit", func(t *testing.T) {
		r := isolatedgit.NewRepo(t)
		remote := r.AddBareRemote()
		before := r.HeadSHA()

		var out bytes.Buffer

		require.NoError(t, appversion.CommitPush(t.Context(), &git.Repo{Dir: r.Dir}, &out, appversion.CommitPushInput{
			Branch: "main", AuthorName: "Test Bot", AuthorEmail: "bot@example.invalid", Message: "noop", FilePattern: "CHANGELOG.md", ExpectedSHA: before,
		}))
		require.Contains(t, out.String(), "No staged changes")
		require.Equal(t, before, r.HeadSHA())
		require.Equal(t, before, remoteMain(t, remote))
	})

	t.Run("remote moved after authorization", func(t *testing.T) {
		r, remote := commitPushFixture(t)
		authorized := r.HeadSHA()

		other := t.TempDir()
		//nolint:gosec // test infra; both paths are the test's own directories.
		out, err := exec.CommandContext(t.Context(), "git", "clone", "-q", remote, other).CombinedOutput()
		require.NoError(t, err, string(out))

		for _, args := range [][]string{
			{"-c", "user.name=Other", "-c", "user.email=other@example.invalid", "commit", "-q", "--allow-empty", "-m", "another writer"},
			{"push", "-q", "origin", "main"},
		} {
			//nolint:gosec // test infra; fixed git arguments in the test's clone.
			gitOut, gitErr := exec.CommandContext(t.Context(), "git", append([]string{"-C", other}, args...)...).CombinedOutput()
			require.NoError(t, gitErr, string(gitOut))
		}

		moved := remoteMain(t, remote)
		require.NotEqual(t, authorized, moved)

		err = appversion.CommitPush(t.Context(), &git.Repo{Dir: r.Dir}, &bytes.Buffer{}, appversion.CommitPushInput{
			Branch: "main", AuthorName: "Test Bot", AuthorEmail: "bot@example.invalid", Message: "chore(release): v1.0.0", FilePattern: "CHANGELOG.md", ExpectedSHA: authorized,
		})
		require.ErrorIs(t, err, errs.ErrValidation)
		require.Equal(t, moved, remoteMain(t, remote))
		require.Equal(t, authorized, r.HeadSHA())
		require.Empty(t, r.Git("diff", "--cached", "--name-only"))
	})
}
