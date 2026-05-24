// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

//go:build linux

package safeexec

import (
	"bytes"
	"fmt"
	"os"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// procSwapsPath is the kernel-published list of active swap areas.
// Overridable via the REUSABLE_CI_PROC_SWAPS env var purely for
// testability — production code always reads /proc/swaps. The env
// override is internal contract, not user-facing API; it has no flag,
// no docs, no support.
const procSwapsPath = "/proc/swaps"

// SwapEnabled reports whether the running kernel has any active swap
// area, by reading /proc/swaps. /proc/swaps always has a header line;
// any subsequent line is one active swap.
//
// Returns (false, error) when /proc/swaps cannot be read — restricted
// containers, hardened sandboxes, exotic kernels. Callers should soft-
// pass on (false, non-nil err) rather than refuse: claiming swap is
// off requires positive evidence, and the inverse claim requires
// positive evidence too.
func SwapEnabled() (bool, error) {
	path := procSwapsPath
	if override := os.Getenv("REUSABLE_CI_PROC_SWAPS"); override != "" {
		path = override
	}

	body, err := os.ReadFile(path) //nolint:gosec // path is a constant or test-only override.
	if err != nil {
		return false, err
	}

	// /proc/swaps:
	//   Filename Type Size Used Priority
	//   /dev/sda2 partition 2097148 0 -2
	// Header alone → no swap. Two+ lines → swap.
	return bytes.Count(body, []byte{'\n'}) > 1, nil
}

// RequireNoSwap is the policy gate for any subcommand that brings
// decrypted private-key material into the Go heap. It refuses to
// proceed when the kernel has an active swap area, because decrypted
// scalars in the heap could be paged to disk and recovered post-job.
//
// debugAllowSwap is the operator's explicit opt-out, surfaced as
// the --debug-allow-swap CLI flag and resolved by the caller. The
// caller emits a Warning annotation to make the choice visible in
// CI logs.
//
// The fix on a real swap-enabled host is one of:
//
//   - disable swap on the runner (`sudo swapoff -a`)
//   - use a hosted runner (swap off by default)
//   - use an external signing path that doesn't decrypt the key in
//     our process (Sigstore keyless, KMS-backed signing — neither of
//     which reusable-ci itself implements today)
//
// Returns nil when:
//
//   - debugAllowSwap is true (operator opt-out), or
//   - swap is not active, or
//   - /proc/swaps is unreadable (we can't assert swap is on without
//     positive evidence; refusing here would block legitimate
//     restricted-container deployments where the indeterminacy is
//     structural).
//
// Returns errs.ErrInvalidConfig (exit code EX_CONFIG = 78) when
// swap is active and debugAllowSwap is false.
func RequireNoSwap(debugAllowSwap bool) error {
	if debugAllowSwap {
		return nil
	}

	on, err := SwapEnabled()
	if err != nil {
		// Deliberate fail-open: when /proc/swaps isn't readable
		// (containerless test runs, locked-down jail, /proc not mounted)
		// we can't determine swap state. The defensive posture would be
		// to block, but that would refuse to run a great many legitimate
		// CI setups for a check that's already a defence-in-depth layer
		// (mlock + RLIMIT_CORE + PR_SET_DUMPABLE are the primary
		// controls). Adopters in strict-posture environments can
		// surface the underlying error via a separate `SwapEnabled()`
		// call if they want hard guarantees.
		return nil //nolint:nilerr // see comment above
	}

	if !on {
		return nil
	}

	return fmt.Errorf(
		"swap is enabled on this runner; refusing to handle private-key material to prevent swap-page extraction.\n\n"+
			"Fix the runner:\n"+
			"  • disable swap: `sudo swapoff -a` (persist via /etc/fstab)\n"+
			"  • OR run release-signing on a hosted runner (GHA-hosted has swap off by default)\n\n"+
			"Or sign your release outside reusable-ci using a path that doesn't decrypt the key in our process:\n"+
			"  • Sigstore keyless (cosign + OIDC) — see https://docs.sigstore.dev\n"+
			"  • KMS-backed signing (AWS KMS / GCP KMS / TPM)\n"+
			"  (reusable-ci does not yet provide these signing paths natively)\n\n"+
			"Debug-only escape hatch (LOCAL USE ONLY — NOT for production releases):\n"+
			"  reusable-ci release sign --debug-allow-swap\n"+
			"  When set, the policy is bypassed and a Warning annotation is emitted.\n\n"+
			"Background: docs/verification.md#swap-policy: %w",
		errs.ErrInvalidConfig,
	)
}
