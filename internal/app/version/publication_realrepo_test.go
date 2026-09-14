//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	gitadapter "github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/isolatedgit"
	"github.com/stretchr/testify/require"
)

// TestCommitPush_CommitsExactlyThePathspecExpansion answers the question the
// recording tests could only restate: the app hands a pathspec list to the
// adapter, but what Git actually stages from a glob or a directory prefix is
// Git's decision, not ours. The recorded argv proves we asked; only a real
// repository proves what landed.
//
// The dirty file outside the pattern is the load-bearing half. A release commit
// that swept up an unrelated edit would still satisfy every "the declared paths
// are present" assertion; it fails only against an exact set.
func TestCommitPush_CommitsExactlyThePathspecExpansion(t *testing.T) {
	repo := isolatedgit.NewRepo(t)
	repo.AddFile("CHANGELOG.md", "# changelog\n", "chore: seed changelog")
	repo.AddFile("charts/app/Chart.yaml", "version: 0.0.0\n", "chore: seed chart")
	repo.AddFile("charts/app/values.yaml", "tag: 0.0.0\n", "chore: seed values")
	repo.AddFile("pkg/version.go", "package pkg\n", "chore: seed source")
	repo.AddBareRemote()

	write := func(rel, body string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(repo.Dir, rel), []byte(body), 0o600))
	}

	write("CHANGELOG.md", "# changelog\n\n## v1.2.3\n")
	write("charts/app/Chart.yaml", "version: 1.2.3\n")
	write("charts/app/values.yaml", "tag: 1.2.3\n")
	// Not in the pattern, and not the engine's to publish.
	write("pkg/version.go", "package pkg // scratch edit\n")

	adapter := &gitadapter.Repo{Dir: repo.Dir}
	require.NoError(t, appversion.CommitPush(t.Context(), adapter, io.Discard, appversion.CommitPushInput{
		Branch: "main", FilePattern: "CHANGELOG.md charts",
		Message: "chore: release v1.2.3", AuthorName: "Test Bot", AuthorEmail: "bot@example.invalid",
	}))

	require.Equal(t,
		[]string{"CHANGELOG.md", "charts/app/Chart.yaml", "charts/app/values.yaml"},
		commitPaths(t, repo, "HEAD"),
		"the directory pathspec must expand to both chart files and pull in nothing else")

	require.Equal(t, " M pkg/version.go", repo.Git("status", "--porcelain"),
		"an edit outside the pattern stays uncommitted")
}

// TestCommitPush_OverlappingGlobPathspecsCommitEachPathOnce is the plan shape
// BumpPlan emits for a Maven artifact in a subdirectory next to the root one:
// the artifact's prefixed glob, the root glob that also reaches into that
// subdirectory, and a changelog named twice. Git must stage the union, each
// changed path exactly once, from glob magic rather than literal names. A
// pattern that matches nothing (a Gradle file this project lacks) does not stop
// the patterns after it, and near-miss names and a changed file no pattern
// names stay uncommitted.
func TestCommitPush_OverlappingGlobPathspecsCommitEachPathOnce(t *testing.T) {
	repo := isolatedgit.NewRepo(t)

	seeded := []string{
		"CHANGELOG.md", "pom.xml", "services/api/CHANGELOG.md", "services/api/pom.xml",
		"services/api/core/pom.xml", "services/api/pom.xml.bak", "tools/pom.xml.txt", "services/api/README.md",
	}
	for _, rel := range seeded {
		repo.AddFile(rel, "seed\n", "chore: seed "+rel)
	}

	repo.AddBareRemote()

	for _, rel := range seeded {
		require.NoError(t, os.WriteFile(filepath.Join(repo.Dir, rel), []byte("1.2.3\n"), 0o600))
	}

	adapter := &gitadapter.Repo{Dir: repo.Dir}
	require.NoError(t, appversion.CommitPush(t.Context(), adapter, io.Discard, appversion.CommitPushInput{
		Branch: "main",
		FilePattern: "services/api/CHANGELOG.md :(glob)services/api/**/pom.xml settings.gradle CHANGELOG.md :(glob)**/pom.xml " +
			"CHANGELOG.md :(glob)services/api/**/pom.xml",
		Message: "chore: release v1.2.3", AuthorName: "Test Bot", AuthorEmail: "bot@example.invalid",
	}))

	raw := strings.Fields(repo.Git("show", "--pretty=format:", "--name-only", "HEAD"))
	require.Len(t, raw, 5, "every changed path appears once in the commit: %v", raw)
	require.Equal(t,
		[]string{"CHANGELOG.md", "pom.xml", "services/api/CHANGELOG.md", "services/api/core/pom.xml", "services/api/pom.xml"},
		commitPaths(t, repo, "HEAD"))

	status := strings.Split(repo.Git("status", "--porcelain"), "\n")
	sort.Strings(status)
	require.Equal(t, []string{" M services/api/README.md", " M services/api/pom.xml.bak", " M tools/pom.xml.txt"}, status)
}

// TestCommitPush_PublishesTheCapturedCommitUnderALease exercises the leased
// path against native Git: the lease, the parent check and the push refspec are
// all argv the fakes could only echo back. Here the bare remote either accepts
// the ref or does not.
func TestCommitPush_PublishesTheCapturedCommitUnderALease(t *testing.T) {
	repo := isolatedgit.NewRepo(t)
	repo.AddFile("CHANGELOG.md", "# changelog\n", "chore: seed changelog")
	remote := repo.AddBareRemote()

	authorized := repo.HeadSHA()
	require.NoError(t, os.WriteFile(filepath.Join(repo.Dir, "CHANGELOG.md"), []byte("# changelog\n\n## v1.2.3\n"), 0o600))

	adapter := &gitadapter.Repo{Dir: repo.Dir}
	require.NoError(t, appversion.CommitPush(t.Context(), adapter, io.Discard, appversion.CommitPushInput{
		Branch: "main", FilePattern: "CHANGELOG.md", Message: "chore: release v1.2.3",
		AuthorName: "Test Bot", AuthorEmail: "bot@example.invalid", ExpectedSHA: authorized,
	}))

	created := repo.HeadSHA()
	require.NotEqual(t, authorized, created)
	require.Equal(t, authorized, strings.TrimSpace(repo.Git("rev-parse", created+"^")),
		"the published commit must sit directly on the authorized source")
	require.Equal(t, created, remoteRef(t, repo, remote, "refs/heads/main"),
		"origin/main must end at the exact OID that was captured locally, not a re-resolved HEAD")
}

// TestCommitPush_RefusesWhenTheAuthorizedBranchMovedUnderneath is the reason
// the lease exists. Another writer advances origin/main after the release run
// computed its plan; the run must decline before it changes anything at all,
// including the repo-local author config it would otherwise write.
func TestCommitPush_RefusesWhenTheAuthorizedBranchMovedUnderneath(t *testing.T) {
	repo := isolatedgit.NewRepo(t)
	repo.AddFile("CHANGELOG.md", "# changelog\n", "chore: seed changelog")
	remote := repo.AddBareRemote()

	authorized := repo.HeadSHA()

	// A second clone of the same bare remote pushes first. This is a real
	// competing writer, not a stubbed error return.
	other := isolatedgit.NewRepo(t)
	other.Git("remote", "add", "shared", remote)
	other.Git("fetch", "-q", "shared", "main")
	other.Git("checkout", "-q", "-B", "main", "FETCH_HEAD")
	other.AddCommit("chore: someone else got there first")
	other.Git("push", "-q", "shared", "main")

	require.NoError(t, os.WriteFile(filepath.Join(repo.Dir, "CHANGELOG.md"), []byte("# changelog\n\n## v1.2.3\n"), 0o600))
	before := gitTreeDigest(t, repo.Dir)

	adapter := &gitadapter.Repo{Dir: repo.Dir}
	err := appversion.CommitPush(t.Context(), adapter, io.Discard, appversion.CommitPushInput{
		Branch: "main", FilePattern: "CHANGELOG.md", Message: "chore: release v1.2.3",
		AuthorName: "Test Bot", AuthorEmail: "bot@example.invalid", ExpectedSHA: authorized,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "moved from authorized source")

	require.Equal(t, before, gitTreeDigest(t, repo.Dir),
		"a refusal before mutation must leave no staged index, no author config and no commit behind")
	require.NotEqual(t, authorized, remoteRef(t, repo, remote, "refs/heads/main"),
		"the other writer's commit must still be the one on origin/main")
}

// commitPaths lists the paths a commit changed relative to its first parent,
// sorted, so the assertion is over a set rather than Git's ordering.
func commitPaths(t *testing.T, repo *isolatedgit.Repo, ref string) []string {
	t.Helper()

	raw := repo.Git("show", "--pretty=format:", "--name-only", ref)

	paths := []string{}

	for _, line := range strings.Split(raw, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			paths = append(paths, trimmed)
		}
	}

	sort.Strings(paths)
	require.NotEmpty(t, paths, "an empty diff means the fixture, not the pathspec expansion, is what was measured")

	return paths
}

// remoteRef resolves a ref inside the bare remote through Git itself, so the
// assertion reads the destination repository and is not fooled by whether the
// ref happens to be loose or packed.
func remoteRef(t *testing.T, repo *isolatedgit.Repo, remote, ref string) string {
	t.Helper()

	row := repo.Git("ls-remote", "--", remote, ref)
	require.NotEmpty(t, row, "ref %s is absent from the bare remote", ref)

	sha, name, found := strings.Cut(row, "\t")
	require.True(t, found, "unparsable ls-remote row %q", row)
	require.Equal(t, ref, strings.TrimSpace(name))

	return strings.TrimSpace(sha)
}
