// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package clicolor renders the project's status glyphs — ✓ (success) and
// ✗ (failure) — optionally colorized green/red for an interactive
// terminal.
//
// Color is decided per writer: Check/Cross emit ANSI only when the
// destination writer is a real TTY. A pipe, a redirected file, a CI log,
// a strings.Builder, or a step-summary sink is not a terminal, so those
// always get the plain glyph — ANSI escapes can never leak into machine
// output or a rendered markdown summary. On top of that, color is
// globally suppressed when the user opts out (--no-color), or per the
// cross-tool conventions NO_COLOR / TERM=dumb (clig.dev §Output).
package clicolor

import (
	"io"
	"os"

	"golang.org/x/term"
)

// ANSI SGR codes. Kept unexported — callers go through Check/Cross.
const (
	green = "\033[32m"
	red   = "\033[31m"
	reset = "\033[0m"
)

// Success and Failure are the canonical plain glyphs, exposed so
// non-writer-aware call sites (e.g. building a markdown summary string)
// can use the same symbols without the color machinery.
const (
	Success = "✓"
	Failure = "✗"
)

// disabled is the process-wide hard-off switch. Its initial value honours
// the standard opt-out env vars so color is suppressed even when the root
// command's flag wiring is bypassed (library use, tests). --no-color
// flips it via Disable. There is deliberately no re-enable: an explicit
// opt-out must win.
//
//nolint:gochecknoglobals // process-wide color policy, resolved once.
var disabled = os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb"

// Disable turns color off for the rest of the process. Called by the root
// command when --no-color is passed.
func Disable() { disabled = true }

// Check returns the success glyph, green when w is an interactive
// terminal and color is enabled, otherwise the plain glyph.
func Check(w io.Writer) string { return colorize(w, green, Success) }

// Cross returns the failure glyph, red under the same rule.
func Cross(w io.Writer) string { return colorize(w, red, Failure) }

func colorize(w io.Writer, code, glyph string) string {
	if disabled || !isTerminal(w) {
		return glyph
	}

	return code + glyph + reset
}

// isTerminal reports whether w is backed by an interactive terminal. Only
// *os.File (and equivalents exposing Fd) can be; everything else — string
// builders, sink files, pipes — is not.
func isTerminal(w io.Writer) bool {
	f, ok := w.(interface{ Fd() uintptr })
	if !ok {
		return false
	}

	return term.IsTerminal(int(f.Fd()))
}
