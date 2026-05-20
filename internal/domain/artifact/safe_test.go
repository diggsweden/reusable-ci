// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package artifact_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/artifact"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestSafeJoin_RejectsUnsafe(t *testing.T) {
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
			if _, err := artifact.SafeJoin(root, entry); !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("SafeJoin(%q) err = %v, want ErrValidation", entry, err)
			}
		})
	}
}

func TestSafeJoin_AcceptsNested(t *testing.T) {
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

func TestValidateName(t *testing.T) {
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
	err := artifact.ValidateName("evil\x1b[31mRED")
	if err == nil {
		t.Fatal("expected rejection")
	}

	if strings.ContainsRune(err.Error(), '\x1b') {
		t.Errorf("error message leaked a raw ESC byte: %q", err.Error())
	}
}
