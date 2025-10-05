//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gitadapter "github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/isolatedgit"
	"github.com/stretchr/testify/require"
)

// TestTagRelease_CreatesOnceAndRerunsAsANoOp is the create-once claim executed
// against native Git rather than a recorder. The rerun matters because Git will
// happily be told to make a tag that already exists and fail, or be told to
// force it and succeed; neither is the supported behaviour, and only a second
// real invocation over real refs distinguishes them.
//
// One limit, stated because a fault replay found it: making the rerun push the
// tag again anyway does not fail here. Git answers an identical tag push with
// "Everything up-to-date" and writes nothing, so the redundant publication is
// invisible in the destination's state. Call cardinality is where that is
// observable, and TestTagRelease_ExactRemoteTagIsIdempotent catches it there.
// What this test adds is what only real Git can show: the object is annotated,
// it is the same object on both sides, and the rerun does not replace it.
func TestTagRelease_CreatesOnceAndRerunsAsANoOp(t *testing.T) {
	repo := isolatedgit.NewRepo(t)
	repo.AddFile("CHANGELOG.md", "# changelog\n", "chore: seed changelog")
	remote := repo.AddBareRemote()

	head := repo.HeadSHA()
	adapter := &gitadapter.Repo{Dir: repo.Dir}
	in := appversion.TagReleaseInput{Tag: "v1.2.3", Signed: false}

	out, err := appversion.TagRelease(t.Context(), adapter, in, nil, io.Discard)
	require.NoError(t, err)
	require.Equal(t, head, out.ReleaseSHA)

	require.Equal(t, "tag", repo.Git("cat-file", "-t", "refs/tags/v1.2.3"),
		"the release tag must be annotated, not lightweight")

	tagObject := repo.Git("rev-parse", "refs/tags/v1.2.3")
	require.Equal(t, tagObject, remoteRef(t, repo, remote, "refs/tags/v1.2.3"),
		"the remote must hold the same tag object, not a re-created one")

	var narration strings.Builder

	rerun, err := appversion.TagRelease(t.Context(), adapter, in, nil, &narration)
	require.NoError(t, err, "an exact rerun is idempotent")
	require.Equal(t, head, rerun.ReleaseSHA)
	require.Contains(t, narration.String(), "rerun is a no-op")
	require.Equal(t, tagObject, repo.Git("rev-parse", "refs/tags/v1.2.3"),
		"a rerun must not recreate the tag object")
	require.Equal(t, tagObject, remoteRef(t, repo, remote, "refs/tags/v1.2.3"))
}

// TestTagRelease_RefusesARemoteTagThatAlreadyClaimsAnotherCommit is the
// concurrency outcome this engine actually supports, stated as a test so it
// stops being an assumption.
//
// A competing writer publishes the release tag at a different commit while the
// run is in flight. There is no compensation and no rollback: the branch commit
// this run already pushed stays published. What is guaranteed is narrower and
// worth having — the run refuses, and it does not move or force the tag that
// the other writer created. The assertions below say exactly that, including
// the part that is not cleaned up.
func TestTagRelease_RefusesARemoteTagThatAlreadyClaimsAnotherCommit(t *testing.T) {
	repo := isolatedgit.NewRepo(t)
	repo.AddFile("CHANGELOG.md", "# changelog\n", "chore: seed changelog")
	remote := repo.AddBareRemote()

	authorized := repo.HeadSHA()

	// The release run publishes its bump commit first, as production does.
	require.NoError(t, os.WriteFile(filepath.Join(repo.Dir, "CHANGELOG.md"), []byte("# changelog\n\n## v1.2.3\n"), 0o600))

	adapter := &gitadapter.Repo{Dir: repo.Dir}
	require.NoError(t, appversion.CommitPush(t.Context(), adapter, io.Discard, appversion.CommitPushInput{
		Branch: "main", FilePattern: "CHANGELOG.md", Message: "chore: release v1.2.3",
		AuthorName: "Test Bot", AuthorEmail: "bot@example.invalid", ExpectedSHA: authorized,
	}))

	bump := repo.HeadSHA()

	// Only now does someone else claim v1.2.3, at an unrelated commit.
	other := isolatedgit.NewRepo(t)
	other.Git("remote", "add", "shared", remote)
	other.AddCommit("chore: an unrelated commit")
	other.AddTag("v1.2.3", "someone else's v1.2.3")
	other.Git("push", "-q", "shared", "refs/tags/v1.2.3")

	foreignTag := other.Git("rev-parse", "refs/tags/v1.2.3")

	_, err := appversion.TagRelease(t.Context(), adapter, appversion.TagReleaseInput{Tag: "v1.2.3"}, nil, io.Discard)
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing to move it")

	require.Equal(t, foreignTag, remoteRef(t, repo, remote, "refs/tags/v1.2.3"),
		"the other writer's tag must survive untouched")
	require.Equal(t, "", localTagObject(t, repo, "v1.2.3"),
		"a refused release must not leave a local tag behind either")

	// The documented, uncompensated half: the branch commit stays published.
	require.Equal(t, bump, remoteRef(t, repo, remote, "refs/heads/main"),
		"there is no rollback of the already-published bump commit; this is the supported outcome, not an accident")
}

// localTagObject returns the local tag name if it exists, or "" when it does
// not, so the "no tag was left behind" assertion cannot pass merely because the
// ref was packed.
func localTagObject(t *testing.T, repo *isolatedgit.Repo, tag string) string {
	t.Helper()

	return strings.TrimSpace(repo.Git("tag", "--list", tag))
}
