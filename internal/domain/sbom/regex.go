// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package sbom

import "regexp"

// regexpCompile is a tiny wrapper so the buildbom matcher doesn't have
// to import regexp directly (keeps the test surface focused).
func regexpCompile(pattern string) (*regexp.Regexp, error) {
	return regexp.Compile(pattern)
}
