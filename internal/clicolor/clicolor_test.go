// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package clicolor_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/clicolor"
)

func TestCheckCross_PlainForNonTerminalWriter(t *testing.T) {
	// A bytes.Buffer is not a TTY, so output must be plain — this is the
	// invariant that keeps ANSI out of pipes, CI logs, files, and summaries.
	var buf bytes.Buffer

	if got := clicolor.Check(&buf); got != "✓" {
		t.Errorf("Check(non-tty) = %q, want plain ✓", got)
	}

	if got := clicolor.Cross(&buf); got != "✗" {
		t.Errorf("Cross(non-tty) = %q, want plain ✗", got)
	}

	if strings.ContainsRune(clicolor.Check(&buf), '\033') {
		t.Error("Check must not emit ANSI for a non-terminal writer")
	}
}

func TestConstants(t *testing.T) {
	if clicolor.Success != "✓" || clicolor.Failure != "✗" {
		t.Errorf("glyph constants drifted: %q / %q", clicolor.Success, clicolor.Failure)
	}
}
