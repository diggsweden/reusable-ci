// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestPrepareNotes_CopiesSourceWhenPresent(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	src := filepath.Join(dir, "src.md")
	tgt := filepath.Join(dir, "release-notes.md")
	body := "## v1.0.0\n- thing\n"
	fsys.WriteFile("src.md", []byte(body))

	var buf bytes.Buffer

	err := apprelease.PrepareNotes(context.Background(), &buf, apprelease.PrepareNotesInput{
		SourceFile: src, TargetFile: tgt,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, tgt); got != body {
		t.Errorf("target = %q, want copy of source", got)
	}

	if !strings.Contains(buf.String(), "Using git-cliff generated release notes") {
		t.Errorf("output = %q", buf.String())
	}

	if !strings.Contains(buf.String(), "Changelog artifact found") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestPrepareNotes_EmptySourceFallsBackToVersionStub(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	src := filepath.Join(dir, "src.md")
	tgt := filepath.Join(dir, "release-notes.md")
	// A 0-byte source (git-cliff produced no commits in range) must not be
	// copied verbatim — that would publish empty notes. It falls through to
	// the version stub.
	fsys.WriteFile("src.md", []byte{})

	var buf bytes.Buffer

	err := apprelease.PrepareNotes(context.Background(), &buf, apprelease.PrepareNotesInput{
		SourceFile: src, TargetFile: tgt, ReleaseVersion: "v1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, tgt); !strings.Contains(got, "# Release v1.0.0") {
		t.Errorf("target = %q, want fallback stub", got)
	}

	if !strings.Contains(buf.String(), "creating fallback") {
		t.Errorf("output = %q, want fallback path", buf.String())
	}
}

// TestPrepareNotes_StubCitesTheCommitOnlyWhenGivenOne covers the version stub
// written when there is no source to copy. It always carries the version
// heading; the commit line appears only when a commit was supplied, so the
// published notes never claim to come from a commit nobody named.
func TestPrepareNotes_StubCitesTheCommitOnlyWhenGivenOne(t *testing.T) {
	t.Parallel()

	const commitLine = "Release created from commit"

	for name, commit := range map[string]string{
		"commit supplied": "abcdef0",
		"no commit":       "",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fsys := testfs.NewReal(t)
			dir := fsys.Root
			tgt := filepath.Join(dir, "release-notes.md")

			var buf bytes.Buffer

			err := apprelease.PrepareNotes(context.Background(), &buf, apprelease.PrepareNotesInput{
				SourceFile:     filepath.Join(dir, "missing.md"),
				TargetFile:     tgt,
				ReleaseVersion: "v1.0.0", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				ReleaseCommit:  commit,
			})
			if err != nil {
				t.Fatal(err)
			}

			got := readFile(t, tgt)
			if !strings.Contains(got, "# Release v1.0.0") {
				t.Errorf("target missing the version heading\nfull: %s", got)
			}

			switch cited := strings.Contains(got, commitLine+" "+commit); {
			case commit != "" && !cited:
				t.Errorf("target should cite commit %q\nfull: %s", commit, got)
			case commit == "" && strings.Contains(got, commitLine):
				t.Errorf("target should not mention a commit\nfull: %s", got)
			}

			if !strings.Contains(buf.String(), "creating fallback") {
				t.Errorf("output = %q", buf.String())
			}
		})
	}
}

// TestPrepareNotes_LeavesAnEmptyFileRatherThanNoFile is the last rung of the
// precedence: no source to copy and no version to stub. It still creates the
// target, because everything downstream is entitled to a notes file existing.
func TestPrepareNotes_LeavesAnEmptyFileRatherThanNoFile(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	tgt := filepath.Join(dir, "release-notes.md")

	err := apprelease.PrepareNotes(context.Background(), &bytes.Buffer{}, apprelease.PrepareNotesInput{
		SourceFile: filepath.Join(dir, "missing.md"),
		TargetFile: tgt,
	})
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(tgt)
	if err != nil {
		t.Fatal(err)
	}

	if info.Size() != 0 {
		t.Errorf("target should be empty, got %d bytes", info.Size())
	}
}

func TestPrepareNotes_DefaultsForFileNames(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	if err := apprelease.PrepareNotes(context.Background(), &bytes.Buffer{}, apprelease.PrepareNotesInput{
		ReleaseVersion: "v1.0.0",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat("release-notes.md"); err != nil {
		t.Errorf("default target not created: %v", err)
	}
}

func TestVerifyChangelog_PrintsPreview(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	path := filepath.Join(dir, "CHANGELOG.md")
	body := "line 1\nline 2\nline 3\n"
	fsys.WriteFile("CHANGELOG.md", []byte(body))

	var buf bytes.Buffer

	err := apprelease.VerifyChangelog(context.Background(), &buf, apprelease.VerifyChangelogInput{
		ChangelogFile: path,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"Changelog generated", "File size:", "Line count: 3", "Preview", "line 1"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("output missing %q\nfull: %s", want, buf.String())
		}
	}
}

func TestVerifyChangelog_MissingFails(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)

	err := apprelease.VerifyChangelog(context.Background(), &bytes.Buffer{}, apprelease.VerifyChangelogInput{
		ChangelogFile: fsys.Path("missing.md"),
	})
	if err == nil || !strings.Contains(err.Error(), "no changelog generated") {
		t.Errorf("err = %v", err)
	}
}

func TestVerifyChangelog_EmptyPathErrors(t *testing.T) {
	t.Parallel()

	err := apprelease.VerifyChangelog(context.Background(), &bytes.Buffer{}, apprelease.VerifyChangelogInput{})
	if err == nil || !strings.Contains(err.Error(), "changelog file is required") {
		t.Errorf("err = %v", err)
	}
}
