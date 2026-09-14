// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cliio_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestReadFile_FromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.txt")

	want := []byte("hello\nworld\n")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := cliio.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != string(want) {
		t.Errorf("ReadFile = %q, want %q", got, want)
	}
}

func TestReadFile_MissingClassifiesAsMissingInput(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.txt")

	_, err := cliio.ReadFile(missing)
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}

	// The original os error detail is preserved (so the operator sees the
	// path)...
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err should wrap fs.ErrNotExist, got %v", err)
	}

	// ...and it's classified so main()'s ladder maps it to EX_NOINPUT (66)
	// rather than the internal-bug default (EX_SOFTWARE 70).
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Errorf("err should wrap errs.ErrMissingInput, got %v", err)
	}
}

func TestWriteFile_AtomicReplaceCorrectPermNoLeftover(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.json")

	if err := cliio.WriteFile(path, []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Overwrite with different content + perm; must fully replace.
	if err := cliio.WriteFile(path, []byte("v2-is-longer"), 0o400); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != "v2-is-longer" {
		t.Errorf("content = %q, want %q", got, "v2-is-longer")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != 0o400 {
		t.Errorf("perm = %v, want 0400", info.Mode().Perm())
	}

	// The temp+rename must leave no stray ".tmp-" sidecars behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

func TestWriteFile_FailureLeavesExistingIntact(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("read-only directory permissions are not enforced for root")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "f.json")

	if err := os.WriteFile(path, []byte("ORIGINAL"), 0o600); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	// A read-only directory makes the temp creation fail; the original
	// file must survive untouched (the atomicity guarantee that keeps a
	// re-run recoverable). Directories need the exec bit, hence 0o500/0o700.
	if err := os.Chmod(dir, 0o500); err != nil { //nolint:gosec // intentionally read-only directory for the test
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // restore writable test dir

	if err := cliio.WriteFile(path, []byte("NEW"), 0o600); err == nil {
		t.Fatal("expected write to fail when the temp cannot be created")
	}

	_ = os.Chmod(dir, 0o700) //nolint:gosec // restore writable test dir

	got, err := os.ReadFile(path) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != "ORIGINAL" {
		t.Errorf("existing file corrupted on failed write: %q", got)
	}
}

func TestReadFile_DirectoryClassifiesAsMissingInput(t *testing.T) {
	// Pointing --file at a directory is an operator path mistake, not an
	// internal bug — it must classify (EX_NOINPUT), not fall through to
	// the EX_SOFTWARE default.
	_, err := cliio.ReadFile(t.TempDir())
	if err == nil {
		t.Fatal("expected an error reading a directory as a file")
	}

	if !errors.Is(err, errs.ErrMissingInput) {
		t.Errorf("err should wrap errs.ErrMissingInput, got %v", err)
	}

	if !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("err should explain the path is a directory, got %v", err)
	}
}

func TestReadFile_StdSentinelRefusesCharDevice(t *testing.T) {
	// /dev/null is a character device on the platforms we target.
	// Substituting it for os.Stdin exercises the clig.dev guard
	// without needing an actual TTY in the test process.
	f, err := os.Open(os.DevNull) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = f.Close() })

	orig := os.Stdin
	os.Stdin = f

	t.Cleanup(func() { os.Stdin = orig })

	if !cliio.StdinIsCharDevice() {
		t.Error("StdinIsCharDevice = false for the opened null device")
	}

	body, err := cliio.ReadFile(cliio.StdSentinel)
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("err = %v, want wrapped errs.ErrUsage", err)
	}

	if body != nil {
		t.Errorf("refused character device returned data: %q", body)
	}
}

func TestReadFile_StdSentinelReadsStdin(t *testing.T) {
	r, w, err := os.Pipe() //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = r.Close() })

	orig := os.Stdin
	os.Stdin = r

	t.Cleanup(func() { os.Stdin = orig })

	go func() {
		defer func() { _ = w.Close() }()

		_, _ = w.WriteString("piped content")
	}()

	got, err := cliio.ReadFile(cliio.StdSentinel)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != "piped content" {
		t.Errorf("ReadFile = %q, want piped content", got)
	}
}

func TestWriteFile_ToDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	if err := cliio.WriteFile(path, []byte("contents"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path) //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != "contents" {
		t.Errorf("file = %q, want contents", got)
	}
}

func TestWriteFile_StdSentinelWritesStdout(t *testing.T) {
	got := captureStdout(t, func() {
		if err := cliio.WriteFile(cliio.StdSentinel, []byte("from-write"), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	if got != "from-write" {
		t.Errorf("stdout = %q, want from-write", got)
	}
}

func TestCreateWriter_StreamsToDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stream.txt")

	w, err := cliio.CreateWriter(path, 0o600) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		t.Fatal(err)
	}

	if _, writeErr := w.Write([]byte("chunk-1 ")); writeErr != nil {
		t.Fatal(writeErr)
	}

	if _, writeErr := w.Write([]byte("chunk-2")); writeErr != nil {
		t.Fatal(writeErr)
	}

	if closeErr := w.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	got, err := os.ReadFile(path) //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != "chunk-1 chunk-2" {
		t.Errorf("file = %q", got)
	}
}

func TestCreateWriter_StdSentinelStreamsStdout(t *testing.T) {
	got := captureStdout(t, func() {
		w, err := cliio.CreateWriter(cliio.StdSentinel, 0o600) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if err != nil {
			t.Fatal(err)
		}

		if _, err := w.Write([]byte("streamed-")); err != nil {
			t.Fatal(err)
		}

		if _, err := w.Write([]byte("stdout")); err != nil {
			t.Fatal(err)
		}
		// Stdout closer is a no-op; safe to defer-and-call.
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	})
	if got != "streamed-stdout" {
		t.Errorf("stdout = %q", got)
	}
}

// captureStdout swaps os.Stdout for the duration of fn and returns
// everything written to the original stdout.
//
// Both pipe ends are registered for close. The read end used to be left open:
// reading to EOF finishes the read but does not release the descriptor, so
// every call leaked one for the lifetime of the test binary. That is invisible
// until a package makes enough calls to reach the process limit, at which
// point the failure lands on whichever unrelated test next opens a file.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe() //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})

	orig := os.Stdout
	os.Stdout = w

	defer func() { os.Stdout = orig }()

	fn()

	_ = w.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}

	return buf.String()
}

// TestReadFile_UnreadableFileClassifiesAsPermissionDenied covers the branch of
// classifyReadError that nothing executed.
//
// ReadFile distinguishes "the file is not there" from "the file is there and
// this process may not read it", and only the first was tested. They are
// different operator actions — create the input, versus fix its mode or the
// job's user — and the second is what a checkout with restrictive permissions
// or a root-owned mount produces.
func TestReadFile_UnreadableFileClassifiesAsPermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: mode bits do not deny access, so this branch is unreachable")
	}

	path := filepath.Join(t.TempDir(), "secret.json")
	if err := os.WriteFile(path, []byte("{}"), 0o000); err != nil {
		t.Fatal(err)
	}

	got, err := cliio.ReadFile(path)
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}

	if got != nil {
		t.Errorf("a refused read returned %q", got)
	}

	// The cause survives the classification: an operator reading the CI log
	// needs the path and the syscall, not just "permission denied".
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("the underlying cause was replaced: %v", err)
	}

	if !strings.Contains(err.Error(), path) {
		t.Errorf("the error does not name the file: %v", err)
	}
}

// TestAppendLines_RefusesLineBreaks pins the one rule the runner env/path
// files need: an entry is a line, so an embedded newline (which would land a
// second, unrequested entry in $GITHUB_ENV) is refused before any write.
func TestAppendLines_RefusesLineBreaks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env")

	if err := cliio.AppendLines(path, "A=1", "B=2"); err != nil {
		t.Fatalf("AppendLines: %v", err)
	}

	if err := cliio.AppendLines(path, "C=3"); err != nil {
		t.Fatalf("second AppendLines: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if want := "A=1\nB=2\nC=3\n"; string(got) != want {
		t.Errorf("file = %q, want %q", got, want)
	}

	err = cliio.AppendLines(path, "D=4\nEVIL=1")
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation for an entry with a line break", err)
	}

	if after, _ := os.ReadFile(path); string(after) != string(got) {
		t.Errorf("file changed by a refused append: %q", after)
	}
}
