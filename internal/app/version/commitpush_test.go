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
	body := r.Git("log", "-1", "--format=%B")
	for _, want := range []string{"chore(release): v1.0.0", "body line", "[skip ci]"} {
		if !strings.Contains(body, want) {
			t.Errorf("commit body missing %q: %q", want, body)
		}
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

func TestCommitPush_RequiredFieldErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]appversion.CommitPushInput{
		"BRANCH":              {AuthorName: "n", AuthorEmail: "e", Message: "m", FilePattern: "x"},
		"COMMIT_AUTHOR_NAME":  {Branch: "main", AuthorEmail: "e", Message: "m", FilePattern: "x"},
		"COMMIT_AUTHOR_EMAIL": {Branch: "main", AuthorName: "n", Message: "m", FilePattern: "x"},
		"COMMIT_MESSAGE":      {Branch: "main", AuthorName: "n", AuthorEmail: "e", FilePattern: "x"},
		"FILE_PATTERN":        {Branch: "main", AuthorName: "n", AuthorEmail: "e", Message: "m"},
	}
	for missing, in := range tests {
		missing, in := missing, in
		t.Run("missing-"+missing, func(t *testing.T) {
			t.Parallel()
			err := appversion.CommitPush(context.Background(), &git.Repo{}, &bytes.Buffer{}, in)
			if err == nil || !strings.Contains(err.Error(), missing) {
				t.Errorf("err = %v, want substring %q", err, missing)
			}
		})
	}
}
