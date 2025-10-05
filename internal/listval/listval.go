// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package listval splits CLI list-flag values into tokens.
//
// Convention (two rules):
//
//   - Simple token lists — platforms, SBOM layers, build tags, project/build
//     types, artifact types, expected names, … — use Tokens, which accepts
//     commas, spaces, or newlines interchangeably. The same flag then works
//     inline (`a,b,c`) or as a multiline YAML block, and callers don't have to
//     remember a per-flag delimiter.
//   - Content-sensitive lists whose values can legitimately contain those
//     characters (file/path globs, `key=value` pairs, structured CSV like tag
//     rules) do NOT use Tokens — they keep their content-specific delimiter.
package listval

import "strings"

// Tokens splits s on any run of commas, ASCII spaces/tabs, carriage returns, or
// newlines, dropping empty tokens. Order is preserved; duplicates are kept
// (callers dedupe if they need to). It is a strict superset of comma-splitting,
// so existing comma-separated inputs parse unchanged.
func Tokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case ',', ' ', '\t', '\n', '\r':
			return true
		default:
			return false
		}
	})
}
