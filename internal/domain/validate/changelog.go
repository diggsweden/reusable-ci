// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate

// ChangelogStat is what the changelog file presence check produces.
// Empty Path / zero LineCount means the file is absent — callers
// decide whether that's an error (full changelog) or a soft fall back
// (minimal changelog).
type ChangelogStat struct {
	Path      string
	Exists    bool
	LineCount int
}

// CountLines returns the number of '\n' separators in content + 1 if
// content is non-empty (matching `wc -l`'s newline-terminator semantics
// for our purposes — an empty file is 0 lines, a single line without
// trailing newline is still 1 line). Used by the full-changelog check.
func CountLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	n := 0
	for _, b := range content {
		if b == '\n' {
			n++
		}
	}
	// File without trailing newline still counts its final line.
	if content[len(content)-1] != '\n' {
		n++
	}
	return n
}
