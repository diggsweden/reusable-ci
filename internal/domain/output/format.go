// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package output defines the output-format registry: the enum of
// renderings the CLI can emit (text / json / github / gitlab). It is
// intentionally pure — no I/O, no adapters — so domain code can decide
// format without pulling in the writer.
//
// "auto" is a request for Resolve to map the active CI platform to the
// matching format; after resolution the effective format is always one
// of the four concrete values. Detecting which platform is active lives
// in package internal/platform (the boundary that reads env), so this
// package never touches env-var strings.
package output

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// Format is the rendering the CLI emits for a given run. Each
// command's writer picks behaviour off it.
type Format string

const (
	// FormatAuto means "resolve from the environment". Callers must
	// call Resolve before branching on the value.
	FormatAuto Format = "auto"

	// FormatText is the default human rendering: plain lines, no
	// machine-parseable structure. Colour and emoji are allowed.
	FormatText Format = "text"

	// FormatJSON is structured JSON to stdout. Callers must NOT
	// interleave free-form text on the same stream; logs belong on
	// stderr via slog.
	FormatJSON Format = "json"

	// FormatGitHub speaks GitHub workflow commands (::error::, ::warning::,
	// ::group::, $GITHUB_OUTPUT k=v writes). Suppresses ANSI.
	FormatGitHub Format = "github"

	// FormatGitLab speaks GitLab's annotation conventions (section_start
	// / section_end, environment-file appends, JUnit attachments).
	FormatGitLab Format = "gitlab"
)

// concrete is the resolved (post-Resolve) format set, in display order.
//
//nolint:gochecknoglobals // resolved-format enumeration — read-only.
var concrete = []Format{FormatText, FormatJSON, FormatGitHub, FormatGitLab}

// All returns every accepted flag value including "auto". Used for the
// urfave flag's help-text and validation.
func All() []Format {
	out := make([]Format, 0, 1+len(concrete))
	out = append(out, FormatAuto)
	out = append(out, concrete...)

	return out
}

// ErrUnknownFormat is returned by Parse when the input doesn't match
// any registered format. The error message lists the accepted values.
var ErrUnknownFormat = errors.New("unknown output format")

// Parse parses a user-supplied string into a Format. Case-insensitive.
// Empty input is rejected — callers should pass FormatAuto explicitly
// rather than relying on zero-value behaviour.
func Parse(s string) (Format, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	lower := strings.ToLower(strings.TrimSpace(s))
	if lower == "" {
		return "", fmt.Errorf("%w: empty (want one of %s)", ErrUnknownFormat, joinFormats(All()))
	}

	for _, f := range All() {
		if string(f) == lower {
			return f, nil
		}
	}

	return "", fmt.Errorf("%w: %q (want one of %s)", ErrUnknownFormat, s, joinFormats(All()))
}

// ParseAndResolve is the common one-shot: parse a user-supplied
// string and immediately resolve FormatAuto for the active platform.
// Returns the concrete (non-auto) Format.
func ParseAndResolve(s string, plat provider.Platform) (Format, error) {
	f, err := Parse(s)
	if err != nil {
		return "", err
	}

	return Resolve(f, plat), nil
}

// Resolve turns FormatAuto into a concrete format based on the active
// CI platform. Non-auto inputs pass through unchanged.
//
// Mapping:
//   - provider.PlatformGitHub → FormatGitHub
//   - provider.PlatformGitLab → FormatGitLab
//   - anything else           → FormatText
//
// JSON is never auto-selected; ask for it explicitly. Callers obtain
// the platform from internal/platform.Detect() (which owns the env-var
// reads); this package stays pure.
func Resolve(f Format, plat provider.Platform) Format {
	if f != FormatAuto {
		return f
	}

	switch plat {
	case provider.PlatformGitHub:
		return FormatGitHub
	case provider.PlatformGitLab:
		return FormatGitLab
	default:
		return FormatText
	}
}

func joinFormats(fs []Format) string {
	s := make([]string, len(fs)) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	for i, f := range fs {
		s[i] = string(f)
	}

	sort.Strings(s)

	return strings.Join(s, ", ")
}
