// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package cliio_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/cliio"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
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

func TestReadFile_MissingSurfacesOSError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.txt")

	_, err := cliio.ReadFile(missing)
	if err == nil || !os.IsNotExist(err) {
		t.Errorf("err = %v, want a wrapped os.ErrNotExist", err)
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

	_, err = cliio.ReadFile(cliio.StdSentinel)
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("err = %v, want wrapped errs.ErrUsage", err)
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
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe() //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		t.Fatal(err)
	}

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
