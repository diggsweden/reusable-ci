// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package safeexec

// SwapState is why the swap policy let a run proceed.
//
// The gate has four ways of saying yes and they are not equivalent to an
// operator, so it reports which one applied instead of collapsing them into a
// bare nil. Rendering is the caller's job: safeexec stays free of output
// concerns, and the CLI layer already owns the annotations.
type SwapState int

const (
	// SwapAbsent means swap state was read and no swap area is active. The
	// policy held on its own terms; nothing to tell the operator.
	SwapAbsent SwapState = iota

	// SwapUndetermined means swap state could not be read at all — a
	// restricted container, a jail without /proc, a non-Linux runner. The
	// policy did not run. This is a deliberate soft-pass (refusing would
	// block many legitimate deployments for a defence-in-depth check), but
	// it is a bypass the operator did not choose, so it is worth saying out
	// loud.
	SwapUndetermined

	// SwapBypassed means an active swap area was found and --debug-allow-swap
	// waived it. The operator chose this one.
	SwapBypassed

	// SwapOverridden means no swap area was reported, but by the file named in
	// REUSABLE_CI_PROC_SWAPS rather than by the kernel. Whoever set it chose
	// the answer, so the pass is not measured kernel state and the caller says
	// so.
	SwapOverridden
)

// String names the state for logs and test failures.
func (s SwapState) String() string {
	switch s {
	case SwapAbsent:
		return "absent"
	case SwapUndetermined:
		return "undetermined"
	case SwapBypassed:
		return "bypassed"
	case SwapOverridden:
		return "overridden"
	}

	return "unknown"
}
