// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
)

func TestSanitizeCell(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, in, want string
	}{
		{name: "plain identifier unchanged", in: "feature/my-branch", want: "feature/my-branch"},
		{name: "newline → space (no row injection)", in: "go\n| INJECTED |", want: "go \\| INJECTED \\|"},
		{name: "carriage return → space", in: "a\rb", want: "a b"},
		{name: "backtick → apostrophe (no code-span breakout)", in: "go`](http://evil)", want: "go'](http://evil)"},
		{name: "pipe escaped (no cell breakout)", in: "a|b", want: `a\|b`},
		{name: "empty stays empty", in: "", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := summary.SanitizeCell(tc.in)
			if got != tc.want {
				t.Errorf("SanitizeCell(%q) = %q, want %q", tc.in, got, tc.want)
			}

			// Invariant: output never contains a breakout character.
			if strings.ContainsAny(got, "\n\r`") {
				t.Errorf("output still contains a breakout char: %q", got)
			}
		})
	}
}
