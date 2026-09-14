// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package clicolor

import (
	"os"
	"strings"
	"testing"
)

// The colour decision has two inputs — the process-wide policy and whether the
// writer is a terminal — and only one combination was covered: policy enabled,
// writer not a terminal, which is what every other test in the suite passes.
// That single case is satisfied by a colorize that returns the plain glyph
// unconditionally, so neither input was actually constraining anything.
//
// The cases below isolate each. The terminal-positive one uses /dev/ptmx: a
// pseudo-terminal master this test opens and closes itself, which is owned
// state rather than the developer's console, and is the only way to reach the
// branch that emits colour at all.

// withPolicy sets the process-wide flag for one test and restores it.
func withPolicy(t *testing.T, off bool) {
	t.Helper()

	previous := disabled
	disabled = off

	t.Cleanup(func() { disabled = previous })
}

// terminalWriter opens a pseudo-terminal and returns it, skipping when the
// platform has none.
func terminalWriter(t *testing.T) *os.File {
	t.Helper()

	f, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal available here: %v", err)
	}

	t.Cleanup(func() { _ = f.Close() })

	if !isTerminal(f) {
		t.Skip("/dev/ptmx opened but is not reported as a terminal")
	}

	return f
}

//nolint:paralleltest // shares process-wide state (the spool directory / the colour policy).
func TestColorize_EmitsColourOnlyForAnEnabledTerminal(t *testing.T) {
	tty := terminalWriter(t)

	t.Run("enabled and a terminal", func(t *testing.T) {
		withPolicy(t, false)

		got := Check(tty)
		if !strings.Contains(got, green) || !strings.Contains(got, reset) {
			t.Errorf("Check = %q, want the green-wrapped glyph", got)
		}

		if !strings.Contains(got, Success) {
			t.Errorf("Check = %q, want it to carry the success glyph", got)
		}

		if cross := Cross(tty); !strings.Contains(cross, red) || !strings.Contains(cross, Failure) {
			t.Errorf("Cross = %q, want the red-wrapped failure glyph", cross)
		}
	})

	t.Run("disabled overrides a terminal", func(t *testing.T) {
		withPolicy(t, true)

		if got := Check(tty); got != Success {
			t.Errorf("Check = %q, want the plain glyph: --no-color must win over a TTY", got)
		}

		if got := Cross(tty); got != Failure {
			t.Errorf("Cross = %q, want the plain glyph", got)
		}
	})

	t.Run("enabled but not a terminal", func(t *testing.T) {
		withPolicy(t, false)

		var plain strings.Builder

		if got := Check(&plain); got != Success {
			t.Errorf("Check = %q, want the plain glyph for a non-terminal writer", got)
		}
	})
}

// TestDisable_IsOneWayForTheProcess pins what the --no-color flag does. It is
// deliberately not reversible: the flag is read once at startup and the policy
// applies to everything the run prints afterwards.
//
//nolint:paralleltest // shares process-wide state (the spool directory / the colour policy).
func TestDisable_IsOneWayForTheProcess(t *testing.T) {
	withPolicy(t, false)

	tty := terminalWriter(t)

	if got := Check(tty); !strings.Contains(got, green) {
		t.Fatalf("precondition: colour is off before Disable (%q)", got)
	}

	Disable()

	if got := Check(tty); got != Success {
		t.Errorf("Check after Disable = %q, want the plain glyph", got)
	}
}

// TestIsTerminal_RejectsWritersWithoutAFileDescriptor covers the type check
// that runs before the syscall: anything that cannot name a descriptor — a
// string builder, a buffer, a sink wrapper — is not a terminal, and asking the
// kernel about it would be a mistake rather than a miss.
func TestIsTerminal_RejectsWritersWithoutAFileDescriptor(t *testing.T) {
	t.Parallel()

	if isTerminal(&strings.Builder{}) {
		t.Error("a strings.Builder was reported as a terminal")
	}

	// An *os.File that is a regular file has a descriptor but is not a tty,
	// so the syscall half of the check is exercised too.
	regular, err := os.CreateTemp(t.TempDir(), "not-a-tty")
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = regular.Close() }()

	if isTerminal(regular) {
		t.Error("a regular file was reported as a terminal")
	}
}
