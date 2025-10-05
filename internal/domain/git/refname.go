// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-git/go-git/v5/plumbing"
)

// ValidRefName accepts fully qualified literal refs, never revision expressions
// or patterns. Terminal controls are refused even where Git would permit them.
func ValidRefName(ref string) bool {
	return strings.HasPrefix(ref, "refs/") && utf8.ValidString(ref) &&
		!strings.ContainsFunc(ref, unicode.IsControl) && plumbing.ReferenceName(ref).Validate() == nil
}
