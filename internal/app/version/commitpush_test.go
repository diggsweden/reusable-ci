//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/isolatedgit"
)

func TestCommitPush_HappyPath(t *testing.T) {
	r := isolatedgit.NewRepo(t)
	r.AddBareRemote()

	require.NoError(t, os.WriteFile(filepath.Join(r.Dir, "CHANGELOG.md"), []byte("# v1\n"), 0o644))

	repo := &git.Repo{Dir: r.Dir}
	var out bytes.Buffer
	err := appversion.CommitPush(context.Background(), repo, &out, appversion.CommitPushInput{
		Branch:      "main",
		AuthorName:  "Test Bot",
		AuthorEmail: "bot@example.invalid",
		Message:     "chore(release): v1.0.0",
		FilePattern: "CHANGELOG.md",
	})
	if err != nil {
		t.Fatalf("CommitPush: %v", err)
	}
	if log := r.Git("log", "-1", "--format=%s"); log != "chore(release): v1.0.0" {
		t.Errorf("commit subject = %q", log)
	}
	if !strings.Contains(r.Git("log", "-1", "--format=%B"), "Signed-off-by") {
		t.Errorf("commit missing Signed-off-by")
	}
}

func TestCommitPush_RejectsUnrelatedStagedFiles(t *testing.T) {
	r := isolatedgit.NewRepo(t)
	r.AddBareRemote()
	require.NoError(t, os.WriteFile(filepath.Join(r.Dir, "unrelated.txt"), []byte("keep staged"), 0o600))
	r.Git("add", "--", "unrelated.txt")
	require.NoError(t, os.WriteFile(filepath.Join(r.Dir, "CHANGELOG.md"), []byte("requested change"), 0o600))
	before := r.Git("rev-parse", "HEAD")
	staged := r.Git("diff", "--cached", "--name-only")
	err := appversion.CommitPush(t.Context(), &git.Repo{Dir: r.Dir}, &bytes.Buffer{}, appversion.CommitPushInput{Branch: "main", AuthorName: "fixture", AuthorEmail: "fixture@example.invalid", Message: "update", FilePattern: "CHANGELOG.md"})
	require.ErrorIs(t, err, errs.ErrValidation)
	require.Equal(t, before, r.Git("rev-parse", "HEAD"))
	require.Equal(t, staged, r.Git("diff", "--cached", "--name-only"))
}

func TestCommitPush_NoChangesIsNoOp(t *testing.T) {
	r := isolatedgit.NewRepo(t)
	r.AddBareRemote()
	repo := &git.Repo{Dir: r.Dir}

	var out bytes.Buffer
	err := appversion.CommitPush(context.Background(), repo, &out, appversion.CommitPushInput{
		Branch: "main", AuthorName: "Test Bot", AuthorEmail: "bot@example.invalid",
		Message: "noop", FilePattern: "CHANGELOG.md",
	})
	if err != nil {
		t.Fatalf("CommitPush: %v", err)
	}
	if !strings.Contains(out.String(), "No staged changes") {
		t.Errorf("expected no-op message, got %q", out.String())
	}
}

func TestCommitPush_MultiFilePattern(t *testing.T) {
	r := isolatedgit.NewRepo(t)
	r.AddBareRemote()

	require.NoError(t, os.WriteFile(filepath.Join(r.Dir, "CHANGELOG.md"), []byte("c"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(r.Dir, "package.json"), []byte("p"), 0o644))

	repo := &git.Repo{Dir: r.Dir}
	err := appversion.CommitPush(context.Background(), repo, &bytes.Buffer{}, appversion.CommitPushInput{
		Branch: "main", AuthorName: "Test Bot", AuthorEmail: "bot@example.invalid",
		Message: "chore: bump", FilePattern: "CHANGELOG.md package.json",
	})
	if err != nil {
		t.Fatalf("CommitPush: %v", err)
	}
	files := r.Git("show", "--name-only", "--format=", "HEAD")
	for _, want := range []string{"CHANGELOG.md", "package.json"} {
		if !strings.Contains(files, want) {
			t.Errorf("commit missing %q: %q", want, files)
		}
	}
}

func TestCommitPush_MultiLineCommitMessagePreserved(t *testing.T) {
	r := isolatedgit.NewRepo(t)
	r.AddBareRemote()

	require.NoError(t, os.WriteFile(filepath.Join(r.Dir, "CHANGELOG.md"), []byte("# v1\n"), 0o644))

	repo := &git.Repo{Dir: r.Dir}
	err := appversion.CommitPush(context.Background(), repo, &bytes.Buffer{}, appversion.CommitPushInput{
		Branch:      "main",
		AuthorName:  "Test Bot",
		AuthorEmail: "bot@example.invalid",
		Message:     "chore(release): v1.0.0\n\nbody line\n\n[skip ci]",
		FilePattern: "CHANGELOG.md",
	})
	if err != nil {
		t.Fatalf("CommitPush: %v", err)
	}
	// The raw message object, byte for byte: the subject, blank line, body and
	// trailer line exactly as given, then one sign-off naming the author.
	body := r.Git("cat-file", "commit", "HEAD")
	_, message, found := strings.Cut(body, "\n\n")
	if !found {
		t.Fatalf("commit object has no message: %q", body)
	}

	want := "chore(release): v1.0.0\n\nbody line\n\n[skip ci]\n\nSigned-off-by: Test Bot <bot@example.invalid>"
	if strings.TrimRight(message, "\n") != want {
		t.Errorf("commit message = %q, want %q", message, want)
	}
}

func TestCommitPush_GlobPathspec(t *testing.T) {
	r := isolatedgit.NewRepo(t)
	r.AddBareRemote()

	require.NoError(t, os.WriteFile(filepath.Join(r.Dir, "CHANGELOG.md"), []byte("# v1\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(r.Dir, "sub", "deeper"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(r.Dir, "sub", "pom.xml"), []byte("<project/>\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(r.Dir, "sub", "deeper", "pom.xml"), []byte("<project/>\n"), 0o644))

	repo := &git.Repo{Dir: r.Dir}
	err := appversion.CommitPush(context.Background(), repo, &bytes.Buffer{}, appversion.CommitPushInput{
		Branch:      "main",
		AuthorName:  "Test Bot",
		AuthorEmail: "bot@example.invalid",
		Message:     "chore(release): v1.0.0",
		FilePattern: "CHANGELOG.md :(glob)**/pom.xml",
	})
	if err != nil {
		t.Fatalf("CommitPush: %v", err)
	}
	files := r.Git("show", "--name-only", "--format=", "HEAD")
	for _, want := range []string{"CHANGELOG.md", "sub/pom.xml", "sub/deeper/pom.xml"} {
		if !strings.Contains(files, want) {
			t.Errorf("commit missing %q: %q", want, files)
		}
	}
}
