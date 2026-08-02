// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/git"
)

func TestValidCommitSHA(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		strings.Repeat("a", 40):       true, // sha1
		strings.Repeat("0", 64):       true, // sha256
		strings.Repeat("f", 64):       true,
		strings.Repeat("a", 39):       false, // too short
		strings.Repeat("a", 41):       false, // between 40 and 64 is not a real SHA
		strings.Repeat("a", 63):       false,
		strings.Repeat("a", 65):       false, // too long
		strings.Repeat("A", 40):       false, // uppercase rejected
		strings.Repeat("g", 40):       false, // non-hex rejected
		"":                            false,
		strings.Repeat("a", 40) + " ": false, // trailing space rejected
	}

	for in, want := range cases {
		if got := git.ValidCommitSHA(in); got != want {
			t.Errorf("ValidCommitSHA(%q) = %v, want %v", in, got, want)
		}
	}
}
