// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import "strings"

// IsSnapshot reports whether ref ends with "-snapshot" (case-insensitive).
// SNAPSHOT releases bypass authorization checks — they're meant for
// pre-release iteration, not production tagging.
func IsSnapshot(ref string) bool {
	return strings.HasSuffix(strings.ToLower(ref), "-snapshot")
}
