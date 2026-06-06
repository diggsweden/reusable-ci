// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build !linux

package safeexec

// SwapEnabled is the non-Linux stub. Production CI for diggsweden
// runs on Linux runners; on other platforms the swap policy
// degenerates to "we have no observable swap state" — soft-pass.
func SwapEnabled() (bool, error) { return false, nil }

// RequireNoSwap is the non-Linux stub. See the Linux build of this
// file for the policy. The debugAllowSwap parameter is accepted for
// signature symmetry but ignored — there's nothing to refuse on a
// platform where we don't observe swap state.
func RequireNoSwap(debugAllowSwap bool) error { _ = debugAllowSwap; return nil }
