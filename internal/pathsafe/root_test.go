// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pathsafe_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

func TestOpenRootRejectsSymlinkedRootAndAncestor(t *testing.T) {
	t.Parallel()

	base := t.TempDir()

	realDir := filepath.Join(base, "real")
	if err := os.MkdirAll(filepath.Join(realDir, "child"), 0o755); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(base, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatal(err)
	}

	for _, candidate := range []string{link, filepath.Join(link, "child")} {
		if root, err := pathsafe.OpenRoot(candidate); !errors.Is(err, errs.ErrValidation) {
			if root != nil {
				_ = root.Close()
			}

			t.Errorf("OpenRoot(%q) error = %v, want ErrValidation", candidate, err)
		}
	}
}

func TestMkdirRootCreatesAndOpensRealDirectories(t *testing.T) {
	t.Parallel()

	dest := filepath.Join(t.TempDir(), "one", "two")

	root, err := pathsafe.MkdirRoot(dest, 0o755)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = root.Close() }()

	if err := root.WriteFile("value", []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}

	if body, err := os.ReadFile(filepath.Join(dest, "value")); err != nil || string(body) != "ok" {
		t.Fatalf("created file = %q, %v", body, err)
	}
}

func TestMkdirRootRejectsSymlinkedMissingPathAncestor(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	outside := t.TempDir()

	link := filepath.Join(base, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	root, err := pathsafe.MkdirRoot(filepath.Join(link, "created"), 0o755)
	if root != nil {
		_ = root.Close()
	}

	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("MkdirRoot error = %v, want ErrValidation", err)
	}

	if _, statErr := os.Stat(filepath.Join(outside, "created")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("symlink ancestor was followed: %v", statErr)
	}
}

// The tests above cover the symlink refusals and MkdirRoot's happy path. The
// ordinary inputs are unasserted: that OpenRoot works at all on a plain
// existing directory, that a blank name is refused, that a regular file is not
// accepted as a root, and that MkdirRoot creates with the permissions it was
// asked for. A guard that refused everything would satisfy the symlink tests.

func TestOpenRoot_OpensAnOrdinaryDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "inside"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	root, err := pathsafe.OpenRoot(dir)
	if err != nil {
		t.Fatalf("OpenRoot on a plain directory failed: %v", err)
	}

	defer func() { _ = root.Close() }()

	// The descriptor must actually be rooted at that directory.
	body, err := fs.ReadFile(root.FS(), "inside")
	if err != nil {
		t.Fatalf("read through the root: %v", err)
	}

	if string(body) != "x" {
		t.Errorf("read %q through the root, want %q", body, "x")
	}
}

// TestOpenRoot_AcceptsARelativePath pins that the name is resolved against the
// working directory rather than rejected. Callers pass workspace-relative paths
// ("dist", "build/reports"), so refusing them would break every one.
//
//nolint:paralleltest // t.Chdir changes the process working directory.
func TestOpenRoot_AcceptsARelativePath(t *testing.T) {
	// Not parallel: t.Chdir changes the process working directory.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Chdir(dir)

	root, err := pathsafe.OpenRoot("dist")
	if err != nil {
		t.Fatalf("OpenRoot(%q) failed: %v", "dist", err)
	}

	_ = root.Close()
}

func TestOpenRoot_RejectsBlankAndNonDirectoryNames(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "regular")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, path string
		wantUsage  bool
		why        string
	}{
		{name: "empty", path: "", wantUsage: true, why: "an empty root would resolve to the working directory"},
		{name: "spaces only", path: "   ", wantUsage: true, why: "whitespace is not a path"},
		{name: "a tab", path: "\t", wantUsage: true},
		{name: "a regular file", path: file, why: "a file is not a directory to be rooted at"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root, err := pathsafe.OpenRoot(tc.path)
			if root != nil {
				_ = root.Close()
			}

			if err == nil {
				t.Fatalf("OpenRoot(%q) succeeded: %s", tc.path, tc.why)
			}

			if tc.wantUsage && !errors.Is(err, errs.ErrUsage) {
				t.Errorf("err = %v, want ErrUsage", err)
			}
		})
	}
}

// TestMkdirRoot_CreatesWithTheRequestedPermissions checks the perm argument
// reaches the directories it creates. A root created world-writable would let
// anything on the runner drop files into a staged artifact tree.
func TestMkdirRoot_CreatesWithTheRequestedPermissions(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	target := filepath.Join(base, "a", "b")

	root, err := pathsafe.MkdirRoot(target, 0o700)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = root.Close() }()

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}

	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("created %s with mode %04o, want %04o", target, got, 0o700)
	}
}
