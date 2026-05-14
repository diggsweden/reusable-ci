// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package localfs_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/adapters/localfs"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestFS_FileChecks(t *testing.T) {
	t.Parallel()

	fsys := testfs.NewReal(t)
	empty := fsys.WriteFile("empty.txt", nil)
	nonEmpty := fsys.WriteFile("artifact.jar", []byte("jar"))
	dir := fsys.MkdirAll("dir")
	adapter := localfs.New()

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

func TestFS_FindReleaseArtifactsAndGlobs(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("release-artifacts/app.jar", []byte("jar"))
	fsys.WriteFile("release-artifacts/archive.tar.gz", []byte("tgz"))
	fsys.WriteFile("release-artifacts/original-app.jar", []byte("ignored"))
	fsys.WriteFile("release-artifacts/readme.txt", []byte("ignored"))
	fsys.WriteFile("a.asc", []byte("sig"))
	fsys.WriteFile("b.asc", []byte("sig"))
	fsys.Chdir()

	adapter := localfs.New()
	got := adapter.FindReleaseArtifacts(fsys.Path("release-artifacts"))
	want := []string{
		fsys.Path("release-artifacts", "app.jar"),
		fsys.Path("release-artifacts", "archive.tar.gz"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("release artifacts = %v, want %v", got, want)
	}
	if got := adapter.FindReleaseArtifacts(fsys.Path("missing")); got != nil {
		t.Errorf("missing dir artifacts = %v, want nil", got)
	}
	if got := adapter.Glob(filepath.Join(fsys.Root, "*.asc")); !reflect.DeepEqual(got, []string{fsys.Path("a.asc"), fsys.Path("b.asc")}) {
		t.Errorf("glob = %v", got)
	}
	if got := adapter.ListASCFiles(); !reflect.DeepEqual(got, []string{"a.asc", "b.asc"}) {
		t.Errorf("asc files = %v", got)
	}
}
