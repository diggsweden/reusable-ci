// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package platform_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appplatform "github.com/diggsweden/reusable-ci/v3/internal/app/platform"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

var errWorkspaceWriterClosed = errors.New("workspace writer closed")

type closedWriter struct{}

func (closedWriter) Write([]byte) (int, error) { return 0, errWorkspaceWriterClosed }

// TestDebugWorkspace_ScriptSectionIsCompleteSortedAndSafe finds validate
// scripts at every depth in lexical path order, skips non-scripts and a
// directory named like a script, quotes a control-bearing script name, and
// produces identical bytes on a second run.
func TestDebugWorkspace_ScriptSectionIsCompleteSortedAndSafe(t *testing.T) {
	t.Parallel()

	fsys := testfs.NewReal(t)
	fsys.WriteFile(".github-shared/z/validate-last.sh", []byte("#!/bin/sh"))
	fsys.WriteFile(".github-shared/a/deep/er/validate-deep.sh", []byte("#!/bin/sh"))
	fsys.WriteFile(".github-shared/validate-top.sh", []byte("#!/bin/sh"))
	fsys.WriteFile(".github-shared/validate-notes.txt", []byte("not a script"))
	fsys.WriteFile(".github-shared/b/validate-bad\nname.sh", []byte("#!/bin/sh"))
	fsys.MkdirAll(".github-shared/validate-dir.sh")

	var first, second bytes.Buffer
	if err := appplatform.DebugWorkspace(&first, appplatform.DebugWorkspaceInput{Root: fsys.Root}); err != nil {
		t.Fatal(err)
	}

	if err := appplatform.DebugWorkspace(&second, appplatform.DebugWorkspaceInput{Root: fsys.Root}); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Errorf("two runs differ:\n%s\n---\n%s", &first, &second)
	}

	shared := filepath.Join(fsys.Root, ".github-shared")

	_, section, _ := strings.Cut(first.String(), "=== Looking for scripts ===\n")
	section, _, _ = strings.Cut(section, "\n=== GitHub context ===")

	want := strings.Join([]string{
		filepath.Join(shared, "a/deep/er/validate-deep.sh"),
		`"` + filepath.Join(shared, "b") + `/validate-bad\nname.sh"`,
		filepath.Join(shared, "validate-top.sh"),
		filepath.Join(shared, "z/validate-last.sh"),
	}, "\n") + "\n"
	if section != want {
		t.Errorf("scripts section = %q, want %q", section, want)
	}
}

// TestDebugWorkspace_RefusalsWriteNothing: a link anywhere inside
// .github-shared, a missing root and an unreadable nested directory are
// refused before any output, and a failing writer is reported.
func TestDebugWorkspace_RefusalsWriteNothing(t *testing.T) {
	t.Parallel()

	linked := testfs.NewReal(t)
	linked.WriteFile(".github-shared/scripts/validate-ok.sh", []byte("#!/bin/sh"))

	if err := os.Symlink(t.TempDir(), linked.Path(".github-shared/scripts/elsewhere")); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer

	err := appplatform.DebugWorkspace(&out, appplatform.DebugWorkspaceInput{Root: linked.Root})
	if !errors.Is(err, errs.ErrValidation) || out.Len() != 0 {
		t.Errorf("nested link: err = %v, output %q", err, &out)
	}

	err = appplatform.DebugWorkspace(&out, appplatform.DebugWorkspaceInput{Root: filepath.Join(t.TempDir(), "absent")})
	if !errors.Is(err, fs.ErrNotExist) || out.Len() != 0 {
		t.Errorf("missing root: err = %v, output %q", err, &out)
	}

	if os.Geteuid() != 0 {
		locked := testfs.NewReal(t)
		locked.WriteFile(".github-shared/locked/validate-hidden.sh", []byte("#!/bin/sh"))

		if chmodErr := os.Chmod(locked.Path(".github-shared/locked"), 0o000); chmodErr != nil {
			t.Fatal(chmodErr)
		}

		t.Cleanup(func() { _ = os.Chmod(locked.Path(".github-shared/locked"), 0o700) }) //nolint:gosec // restores an owned temp directory for cleanup.

		err = appplatform.DebugWorkspace(&out, appplatform.DebugWorkspaceInput{Root: locked.Root})
		if !errors.Is(err, fs.ErrPermission) || out.Len() != 0 {
			t.Errorf("unreadable directory: err = %v, output %q", err, &out)
		}
	}

	if err := appplatform.DebugWorkspace(closedWriter{}, appplatform.DebugWorkspaceInput{Root: testfs.NewReal(t).Root}); !errors.Is(err, errWorkspaceWriterClosed) {
		t.Errorf("writer failure: err = %v", err)
	}
}
