// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate

import "strings"

// IsSnapshot reports whether ref ends with "-snapshot" (case-insensitive).
// SNAPSHOT releases bypass authorization checks — they're meant for
// pre-release iteration, not production tagging.
func IsSnapshot(ref string) bool {
	return strings.HasSuffix(strings.ToLower(ref), "-snapshot")
}
