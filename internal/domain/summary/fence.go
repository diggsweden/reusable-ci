// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary

import "strings"

// CodeFence returns a backtick fence that body cannot close: one backtick
// longer than the longest run inside it, and never shorter than three.
// CommonMark closes a fenced block only on a run at least as long as the
// opening one, so verbatim text containing ``` stays inside the block.
func CodeFence(body string) string {
	longest, run := 0, 0

	for _, char := range body {
		if char == '`' {
			run++
			longest = max(longest, run)

			continue
		}

		run = 0
	}

	return strings.Repeat("`", max(3, longest+1))
}
