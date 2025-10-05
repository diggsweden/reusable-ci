// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package artifact_test

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/artifact"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestCopyAtMost_DetectsOversizeWithoutRejectingExactLimit(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		body      string
		wantError bool
	}{
		{name: "below", body: "1234"},
		{name: "exact", body: "12345"},
		{name: "over", body: "123456", wantError: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var dst bytes.Buffer

			written, err := artifact.CopyAtMost(&dst, strings.NewReader(testCase.body), 5)
			if testCase.wantError && !errors.Is(err, errs.ErrValidation) || !testCase.wantError && err != nil {
				t.Fatalf("CopyAtMost error = %v, want validation=%v", err, testCase.wantError)
			}

			if written != int64(len(testCase.body)) || dst.String() != testCase.body {
				t.Fatalf("copied=%d body=%q, want %q", written, &dst, testCase.body)
			}
		})
	}
}

func TestValidateAggregateBoundsBytesAndFileCount(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		bytes      int64
		files      int
		addBytes   int64
		addFiles   int
		wantReject bool
	}{
		{name: "empty"},
		{name: "below limits", bytes: 1, files: 1, addBytes: 2, addFiles: 2},
		{name: "exact limits", bytes: artifact.MaxTotalBytes - 1, files: artifact.MaxFileCount - 1, addBytes: 1, addFiles: 1},
		{name: "byte overflow", bytes: artifact.MaxTotalBytes, addBytes: 1, wantReject: true},
		{name: "file overflow", files: artifact.MaxFileCount, addFiles: 1, wantReject: true},
		{name: "negative addition", addBytes: -1, wantReject: true},
		{name: "negative file addition", addFiles: -1, wantReject: true},
		{name: "negative starting bytes", bytes: -1, wantReject: true},
		{name: "negative starting files", files: -1, wantReject: true},
		{name: "excess starting bytes", bytes: artifact.MaxTotalBytes + 1, wantReject: true},
		{name: "excess starting files", files: artifact.MaxFileCount + 1, wantReject: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := artifact.ValidateAggregate(testCase.bytes, testCase.files, testCase.addBytes, testCase.addFiles)
			if testCase.wantReject && !errors.Is(err, errs.ErrValidation) || !testCase.wantReject && err != nil {
				t.Fatalf("ValidateAggregate error = %v, want reject=%v", err, testCase.wantReject)
			}
		})
	}
}

type failingCopyWriter struct{ err error }

func (w failingCopyWriter) Write([]byte) (int, error) { return 0, w.err }

func TestCopyAtMost_LimitAndIOBoundaries(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		limit   int64
		body    string
		written int64
		want    error
	}{
		{0, "", 0, nil}, {0, "x", 1, errs.ErrValidation}, {-1, "small", 0, errs.ErrUsage},
		{math.MaxInt64, "small", 0, errs.ErrUsage}, {math.MaxInt64 - 1, "small", 5, nil},
	} {
		var dst bytes.Buffer

		src := strings.NewReader(tc.body)

		written, err := artifact.CopyAtMost(&dst, src, tc.limit)
		if !errors.Is(err, tc.want) || written != tc.written || dst.String() != tc.body[:tc.written] || int64(src.Len()) != int64(len(tc.body))-tc.written {
			t.Fatalf("limit=%d written=%d err=%v body=%q unread=%d", tc.limit, written, err, &dst, src.Len())
		}
	}

	cause := errors.New("owned copy failure") //nolint:err113 // independent I/O cause.

	var dst bytes.Buffer
	if n, err := artifact.CopyAtMost(&dst, iotest.ErrReader(cause), 5); n != 0 || !errors.Is(err, cause) || dst.Len() != 0 {
		t.Fatalf("reader: n=%d err=%v body=%q", n, err, &dst)
	}

	if n, err := artifact.CopyAtMost(failingCopyWriter{cause}, strings.NewReader("small"), 5); n != 0 || !errors.Is(err, cause) {
		t.Fatalf("writer: n=%d err=%v", n, err)
	}
	// Reader EOF is successful exhaustion, not the unrelated error above.
	if n, err := artifact.CopyAtMost(&dst, iotest.ErrReader(io.EOF), 5); n != 0 || err != nil {
		t.Fatalf("empty: n=%d err=%v", n, err)
	}
}

func TestOpenUploadEntry_RejectsReplacementSymlink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	path := filepath.Join(dir, "artifact.txt")
	if err := os.WriteFile(path, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}

	entries, err := artifact.CollectUploadEntries(dir, nil, false)
	if err != nil {
		t.Fatal(err)
	}

	outside := filepath.Join(t.TempDir(), "outside.txt")
	if writeErr := os.WriteFile(outside, []byte("evil"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	if removeErr := os.Remove(path); removeErr != nil {
		t.Fatal(removeErr)
	}

	if linkErr := os.Symlink(outside, path); linkErr != nil {
		t.Fatal(linkErr)
	}

	file, err := artifact.OpenUploadEntry(entries[0])
	if file != nil {
		_ = file.Close()
	}

	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("OpenUploadEntry error = %v, want ErrValidation", err)
	}
}

func TestSafeJoin_RejectsUnsafe(t *testing.T) {
	t.Parallel()

	root := "/tmp/dest"

	for _, entry := range []string{
		"",            // empty
		"/etc/passwd", // absolute
		"../escape",   // traversal
		"a/../../b",   // traversal mid-path
		`a\b`,         // backslash separators
		"a\nb",        // embedded newline
	} {
		t.Run(entry, func(t *testing.T) {
			t.Parallel()

			if _, err := artifact.SafeJoin(root, entry); !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("SafeJoin(%q) err = %v, want ErrValidation", entry, err)
			}
		})
	}
}

func TestSafeJoin_AcceptsNested(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	got, err := artifact.SafeJoin(root, "sub/dir/file.txt")
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(root, "sub", "dir", "file.txt")
	if got != want {
		t.Errorf("SafeJoin = %q, want %q", got, want)
	}
}

func TestValidateName_RejectsControlCharactersAndEmpty(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"dist", "go-build-artifacts", "x-1.2.3_final", "über"} {
		if err := artifact.ValidateName(ok); err != nil {
			t.Errorf("valid name %q rejected: %v", ok, err)
		}
	}

	// Empty, newlines, AND any other control character (ESC, TAB, BS, NUL,
	// DEL, C1) — a downloaded name is echoed into CI logs and becomes a
	// dir/<name>/ path, so a raw escape sequence would be a terminal-spoof
	// vector.
	for _, bad := range []string{
		"",
		"a\nb", "a\rb",
		"evil\x1b[31mRED", // ANSI SGR escape
		"a\tb",            // tab
		"a\x08b",          // backspace
		"a\x00b",          // NUL
		"a\x7fb",          // DEL
		"a\u009bb",        // C1 CSI, properly UTF-8 encoded — a control rune
		"a\x9bb",          // raw 0x9b — invalid UTF-8, rejected as such
		"a\xffb",          // invalid UTF-8 byte
	} {
		if err := artifact.ValidateName(bad); !errors.Is(err, errs.ErrValidation) {
			t.Errorf("ValidateName(%q) err = %v, want ErrValidation", bad, err)
		}
	}
}

// TestValidateName_ErrorMessageIsTerminalSafe proves the rejection message
// itself doesn't leak the raw control bytes — %q renders them escaped — so even
// reporting a hostile name can't spoof the terminal.
func TestValidateName_ErrorMessageIsTerminalSafe(t *testing.T) {
	t.Parallel()

	err := artifact.ValidateName("evil\x1b[31mRED")
	if err == nil {
		t.Fatal("expected rejection")
	}

	if strings.ContainsRune(err.Error(), '\x1b') {
		t.Errorf("error message leaked a raw ESC byte: %q", err.Error())
	}
}

// The reopen guard has a symlink case and nothing else, so what it does when
// the file is FINE, and what it does when the file was swapped for another
// ordinary file, are both unasserted. Those are the cases the identity
// comparison exists for: a symlink is caught by the IsRegular check alone, so
// removing os.SameFile entirely leaves the symlink test passing while a
// same-size regular replacement — a build step rewriting an artifact between
// collection and upload — sails through and gets signed and published as the
// collected one.

// TestOpenUploadEntry_ReopensAnUnchangedFileExactly is the positive control.
// Without it every assertion below is satisfied by a guard that refuses
// everything.
func TestOpenUploadEntry_ReopensAnUnchangedFileExactly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	const body = "collected bytes"

	if err := os.WriteFile(filepath.Join(dir, "artifact.txt"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	entries, err := artifact.CollectUploadEntries(dir, nil, false)
	if err != nil {
		t.Fatal(err)
	}

	file, err := artifact.OpenUploadEntry(entries[0])
	if err != nil {
		t.Fatalf("an unchanged file was refused: %v", err)
	}

	defer func() { _ = file.Close() }()

	got, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != body {
		t.Errorf("reopened content = %q, want %q", got, body)
	}
}

// TestOpenUploadEntry_RefusesARegularReplacement is the case the identity
// comparison is for: same path, same size, same mode, different inode.
//
// Nothing about the replacement is suspicious to look at. It is a regular file
// of exactly the right length, so every check except os.SameFile accepts it —
// which is why the symlink test cannot stand in for this one.
func TestOpenUploadEntry_RefusesARegularReplacement(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "artifact.txt")

	if err := os.WriteFile(path, []byte("collected-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	entries, err := artifact.CollectUploadEntries(dir, nil, false)
	if err != nil {
		t.Fatal(err)
	}

	// Rename a different file over the path. Remove-then-recreate is not
	// reliable here: the filesystem may hand the new file the inode the old
	// one just released, and then it IS the same file by identity. A rename
	// moves an inode that demonstrably already existed alongside the
	// original, which is also closer to how a build step replaces an output.
	replacement := filepath.Join(dir, ".replacement")
	if writeErr := os.WriteFile(replacement, []byte("swapped--bytes!"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	if renameErr := os.Rename(replacement, path); renameErr != nil {
		t.Fatal(renameErr)
	}

	file, err := artifact.OpenUploadEntry(entries[0])
	if file != nil {
		_ = file.Close()
	}

	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("a same-size regular replacement was accepted (err = %v); it would be signed as the collected artifact", err)
	}
}

// TestOpenUploadEntry_RefusesAChangedSize covers the other half independently:
// a file appended to in place keeps its inode, so SameFile still matches and
// only the recorded size catches it.
func TestOpenUploadEntry_RefusesAChangedSize(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "artifact.txt")

	if err := os.WriteFile(path, []byte("collected"), 0o600); err != nil {
		t.Fatal(err)
	}

	entries, err := artifact.CollectUploadEntries(dir, nil, false)
	if err != nil {
		t.Fatal(err)
	}

	appended, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}

	if _, writeErr := appended.WriteString(" and more"); writeErr != nil {
		t.Fatal(writeErr)
	}

	if closeErr := appended.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	file, err := artifact.OpenUploadEntry(entries[0])
	if file != nil {
		_ = file.Close()
	}

	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("a file that grew after collection was accepted (err = %v)", err)
	}
}
