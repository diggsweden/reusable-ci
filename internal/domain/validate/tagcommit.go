// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate

// BranchPosition describes how a tag commit sits relative to a branch.
type BranchPosition string

const (
	// BranchPositionAtHead — the tag points to branch HEAD (ideal).
	BranchPositionAtHead BranchPosition = "at-head"
	// BranchPositionAncestor — tag commit is an ancestor of HEAD (normal
	// for existing releases).
	BranchPositionAncestor BranchPosition = "ancestor"
	// BranchPositionAhead — branch HEAD is an ancestor of the tag commit
	// (commits not yet pushed to the branch).
	BranchPositionAhead BranchPosition = "ahead"
	// BranchPositionDiverged — neither is an ancestor of the other.
	BranchPositionDiverged BranchPosition = "diverged"
)

// IsFatal reports whether this position should fail validation. Ahead
// and Diverged both block the workflow; AtHead and Ancestor pass.
func (p BranchPosition) IsFatal() bool {
	return p == BranchPositionAhead || p == BranchPositionDiverged
}

// ClassifyBranchPosition resolves the relationship from the two
// is-ancestor probes. The arguments mirror what the bash makes:
//
//	tagInBranch     = `git merge-base --is-ancestor <tagCommit> <branchHead>`
//	branchInTag     = `git merge-base --is-ancestor <branchHead> <tagCommit>`
//	tagEqualsBranch = (tagCommit == branchHead)
func ClassifyBranchPosition(tagInBranch, branchInTag, tagEqualsBranch bool) BranchPosition {
	switch {
	case tagEqualsBranch:
		return BranchPositionAtHead
	case tagInBranch:
		return BranchPositionAncestor
	case branchInTag:
		return BranchPositionAhead
	default:
		return BranchPositionDiverged
	}
}
