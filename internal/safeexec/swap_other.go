// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build !linux

package safeexec

// SwapEnabled is the non-Linux stub. Production CI for diggsweden
// runs on Linux runners; on other platforms the swap policy
// degenerates to "we have no observable swap state" — soft-pass.
func SwapEnabled() (bool, error) { return false, nil }

// RequireNoSwap is the non-Linux stub. See the Linux build of this file for
// the policy. The debugAllowSwap parameter is accepted for signature symmetry
// but ignored — there's nothing to waive on a platform where we don't observe
// swap state.
//
// It reports SwapUndetermined rather than SwapAbsent: "we did not look" and
// "we looked and found none" are different claims, and the caller surfaces the
// difference. Saying SwapAbsent here would assert a fact this build cannot
// establish.
func RequireNoSwap(debugAllowSwap bool) (SwapState, error) {
	_ = debugAllowSwap

	return SwapUndetermined, nil
}
