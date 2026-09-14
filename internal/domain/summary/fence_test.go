// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
)

func TestCodeFence_OutlastsEveryBacktickRun(t *testing.T) {
	t.Parallel()

	for body, want := range map[string]string{
		"":                     "```",
		"plain text":           "```",
		"a `code` span":        "```",
		"```\nescaped\n```":    "````",
		"x ``` y ````` z ` w":  "``````",
		"trailing run ````":    "`````",
		"runs `` split ` by x": "```",
	} {
		if got := summary.CodeFence(body); got != want {
			t.Errorf("CodeFence(%q) = %q, want %q", body, got, want)
		}
	}
}
