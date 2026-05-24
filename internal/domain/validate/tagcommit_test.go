// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/validate"
)

func TestClassifyBranchPosition(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		tagInBranch     bool
		branchInTag     bool
		tagEqualsBranch bool
		want            validate.BranchPosition
	}{
		// Equality always wins, even if both probes also fire.
		{"at head", true, true, true, validate.BranchPositionAtHead},
		// Tag is somewhere in branch history.
		{"ancestor", true, false, false, validate.BranchPositionAncestor},
		// Branch HEAD is in the tag's history → tag is ahead.
		{"ahead", false, true, false, validate.BranchPositionAhead},
		// Neither: diverged.
		{"diverged", false, false, false, validate.BranchPositionDiverged},
	}
	for _, c := range cases {
		got := validate.ClassifyBranchPosition(c.tagInBranch, c.branchInTag, c.tagEqualsBranch)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
