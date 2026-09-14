// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package osfs_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/osfs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

//nolint:cyclop // exercises every Stat/Exists/IsDir predicate on one fixture.
func TestFS_FileChecks(t *testing.T) {
	t.Parallel()

	fsys := testfs.NewReal(t)
	empty := fsys.WriteFile("empty.txt", nil)
	nonEmpty := fsys.WriteFile("artifact.jar", []byte("jar"))
	dir := fsys.MkdirAll("dir")
	adapter := osfs.New()

	if !adapter.FileExists(empty) || !adapter.FileExists(nonEmpty) {
		t.Fatal("expected regular files to exist")
	}

	if adapter.FileExists(dir) || adapter.FileExists(fsys.Path("missing")) || adapter.FileExists("") {
		t.Fatal("directories, missing paths, and empty paths should not be regular files")
	}

	if adapter.FileNonEmpty(empty) {
		t.Fatal("empty file should not be non-empty")
	}

	if !adapter.FileNonEmpty(nonEmpty) {
		t.Fatal("non-empty file should be non-empty")
	}

	if adapter.FileNonEmpty(dir) || adapter.FileNonEmpty(fsys.Path("missing")) || adapter.FileNonEmpty("") {
		t.Fatal("directories, missing paths, and empty paths should not be non-empty files")
	}
}

// No t.Parallel(): ListSignatureSidecars globs the working directory, so
// this test has to Chdir, and cwd is process-global.
func TestFS_FindReleaseArtifactsAndGlobs(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("release-artifacts/app.jar", []byte("jar"))
	fsys.WriteFile("release-artifacts/archive.tar.gz", []byte("tgz"))
	fsys.WriteFile("release-artifacts/original-app.jar", []byte("ignored"))
	fsys.WriteFile("release-artifacts/readme.txt", []byte("ignored"))
	fsys.WriteFile("a.asc", []byte("sig"))
	fsys.WriteFile("b.asc", []byte("sig"))
	fsys.WriteFile("c.bundle", []byte("sigstore"))
	fsys.WriteFile("d.bundle.bak", []byte("ignored"))
	fsys.Chdir()

	adapter := osfs.New()
	got := adapter.FindReleaseArtifacts(fsys.Path("release-artifacts"))

	want := []string{
		fsys.Path("release-artifacts", "app.jar"),
		fsys.Path("release-artifacts", "archive.tar.gz"),
	}
	if !slices.Equal(got, want) {
		t.Errorf("release artifacts = %v, want %v", got, want)
	}

	if got := adapter.FindReleaseArtifacts(fsys.Path("missing")); got != nil {
		t.Errorf("missing dir artifacts = %v, want nil", got)
	}

	if got := adapter.Glob(filepath.Join(fsys.Root, "*.asc")); !slices.Equal(got, []string{fsys.Path("a.asc"), fsys.Path("b.asc")}) {
		t.Errorf("glob = %v", got)
	}

	// Both signing methods leave a sidecar: *.asc from gpg and *.bundle from
	// cosign. A listing of only the gpg ones would upload half the evidence.
	if got := adapter.ListSignatureSidecars(); !slices.Equal(got, []string{"a.asc", "b.asc", "c.bundle"}) {
		t.Errorf("signature sidecars = %v, want the gpg and cosign sidecars", got)
	}
}
