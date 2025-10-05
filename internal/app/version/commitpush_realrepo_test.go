//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
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

// TestCommitPush_DryRunLeavesTheRepositoryByteIdentical is the evidence the
// recording tests could not give.
//
// A dry run's promise is that you can run it on a developer's checkout and
// nothing changes. Recording ports prove the app asked for no mutation, which
// is a claim about the app; it says nothing about what the real Git adapter
// does with the same call. `git status` alone can write: it refreshes the index
// stat cache, and with fsmonitor or the untracked cache enabled it writes more.
// So this runs the real adapter against a real repository and compares the
// whole .git directory, byte for byte, before and after.
//
// The wet control is what stops that from being vacuous: the same inputs
// without DryRun must change the repository, or the comparison above would pass
// on a function that does nothing at all.
func TestCommitPush_DryRunLeavesTheRepositoryByteIdentical(t *testing.T) {
	repo := isolatedgit.NewRepo(t)
	repo.AddFile("README.md", "# demo\n", "chore: seed")
	remote := repo.AddBareRemote()
	require.NotEmpty(t, remote)

	// Closer to a real checkout, which may have index-writing features on.
	// Note what this does not establish: with these set, removing the adapter's
	// --no-optional-locks / fsmonitor / untracked-cache flags still leaves the
	// repository byte-identical here, so this fixture does not demonstrate that
	// those flags are load-bearing. It demonstrates the property that matters,
	// that the dry run changes nothing; the flags remain a deliberate belt on a
	// machine where Git would otherwise write.
	repo.Git("config", "core.untrackedCache", "true")
	repo.Git("status", "--porcelain")

	// An uncommitted change is the situation a release run finds itself in.
	require.NoError(t, os.WriteFile(filepath.Join(repo.Dir, "CHANGELOG.md"), []byte("# changelog\n"), 0o600))

	before := gitTreeDigest(t, repo.Dir)

	adapter := &gitadapter.Repo{Dir: repo.Dir}
	err := appversion.CommitPush(t.Context(), adapter, io.Discard, appversion.CommitPushInput{
		Branch:      "main",
		FilePattern: "CHANGELOG.md",
		Message:     "chore: release",
		AuthorName:  "Test Bot",
		AuthorEmail: "bot@example.invalid",
		DryRun:      true,
	})
	require.NoError(t, err)

	require.Equal(t, before, gitTreeDigest(t, repo.Dir),
		"a dry run must leave the index, config, refs and objects byte-identical")
	require.Equal(t, "# changelog\n", readFile(t, filepath.Join(repo.Dir, "CHANGELOG.md")),
		"a dry run must not touch the working tree either")

	// The wet control: the same call without DryRun has to change something,
	// or the comparison above proves nothing.
	require.NoError(t, appversion.CommitPush(t.Context(), adapter, io.Discard, appversion.CommitPushInput{
		Branch:      "main",
		FilePattern: "CHANGELOG.md",
		Message:     "chore: release",
		AuthorName:  "Test Bot",
		AuthorEmail: "bot@example.invalid",
	}))

	require.NotEqual(t, before, gitTreeDigest(t, repo.Dir), "the wet run must actually commit")
	require.Contains(t, repo.Git("log", "-1", "--pretty=%s"), "chore: release")
	require.Empty(t, strings.TrimSpace(repo.Git("status", "--porcelain")), "the wet run commits exactly what it staged")
}

// gitTreeDigest hashes every entry under .git by path, mode and content, so a
// single differing byte anywhere in the repository state shows up as a
// difference. Paths that Git rewrites as a matter of course on any read are not
// excluded: whether they change is exactly the question.
func gitTreeDigest(t *testing.T, dir string) map[string]string {
	t.Helper()

	digests := map[string]string{}
	root := filepath.Join(dir, ".git")

	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if entry.IsDir() {
			digests[rel] = "dir " + info.Mode().String()

			return nil
		}

		body, readErr := os.ReadFile(path) //nolint:gosec // owned temporary repository.
		if readErr != nil {
			return readErr
		}

		sum := sha256.Sum256(body)
		digests[rel] = info.Mode().String() + " " + hex.EncodeToString(sum[:])

		return nil
	}))

	names := make([]string, 0, len(digests))
	for name := range digests {
		names = append(names, name)
	}

	sort.Strings(names)
	require.NotEmpty(t, names, "an empty .git means the fixture, not the adapter, is what was measured")

	return digests
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path) //nolint:gosec // owned temporary repository.
	require.NoError(t, err)

	return string(body)
}
