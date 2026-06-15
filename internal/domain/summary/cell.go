// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary

import (
	"strings"
	"unicode"
)

// SanitizeCell makes an attacker-influenceable value safe to embed in a
// Markdown table cell of a step summary.
//
// Values such as a fork pull request's branch name flow into the PR
// summary, and git allows backticks and pipes in ref names. Without
// neutralising them, a crafted value could break out of its cell:
//
//   - a newline would inject an entirely new table row,
//   - a backtick would close the surrounding code span and let the rest
//     render as Markdown (e.g. a phishing link),
//   - a pipe is the cell delimiter.
//
// SanitizeCell collapses every control character (CR/LF/tab/…) to a
// single space, turns backticks into apostrophes, and escapes pipes.
// Ordinary identifiers pass through unchanged.
func SanitizeCell(s string) string {
	var b strings.Builder //nolint:varnamelen // idiomatic short name for a string builder.

	b.Grow(len(s))

	for _, r := range s { //nolint:varnamelen // idiomatic short name for a rune.
		switch {
		case r == '`':
			b.WriteByte('\'')
		case r == '|':
			b.WriteString(`\|`)
		case unicode.IsControl(r):
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}

	return b.String()
}
