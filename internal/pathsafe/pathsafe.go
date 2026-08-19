// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package pathsafe answers one question: may this path be used as a
// workspace-relative location?
//
// Three copies of that answer had grown — one per caller that needed it —
// and they disagreed. Two split the path into components to find a parent
// step; the third matched substrings, which misses an OS separator. One
// rejected tabs, one rejected them optionally, one not at all. Nothing
// compared them, because nothing could: `domain` needs this and so does
// `app`, and neither may import the other.
//
// That makes it a leaf utility under ADR 0004's third rule, alongside
// listval — carrying no domain knowledge, belonging to no layer, importable
// by all of them. internal/syncguard keeps it the only implementation.
package pathsafe

import (
	"path/filepath"
	"strings"
)

// Relative reports whether path is safe to resolve inside a working
// directory: non-empty, not absolute, containing no parent-directory step,
// and free of characters that would break the line-oriented formats these
// paths travel through (manifests, plan JSON, shell output).
//
// Traversal is found by splitting components rather than by matching
// substrings, so a path is judged the same however its separators are
// written.
func Relative(path string) bool {
	if path == "" || filepath.IsAbs(path) || strings.ContainsAny(path, "\t\n\r") {
		return false
	}

	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return false
		}
	}

	return true
}
