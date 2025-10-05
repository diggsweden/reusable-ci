// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build linux

package safeexec

import (
	"bytes"
	"fmt"
	"os"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// procSwapsPath is the kernel-published list of active swap areas. Unit tests
// in this package point it at a fixture; nothing else changes it.
//
//nolint:gochecknoglobals // package-private test seam for the kernel file.
var procSwapsPath = "/proc/swaps"

// procSwapsOverrideEnv names a file read instead of /proc/swaps. It is the
// swap-state injection point of the black-box suite, which drives the real
// binary. It used to be treated as the kernel's answer; a pass read from it is
// now reported as SwapOverridden, so a job that sets it cannot present fixture
// bytes as measured kernel state without the log saying so.
const procSwapsOverrideEnv = "REUSABLE_CI_PROC_SWAPS"

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
	if override := os.Getenv(procSwapsOverrideEnv); override != "" {
		path = override
	}

	body, err := os.ReadFile(path) //nolint:gosec // the kernel file, a package test fixture or the reported override.
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
// Reports which of the three passing states applied, so the caller can tell
// the operator apart from the log:
//
//   - SwapAbsent: swap state was read and no swap area is active.
//   - SwapBypassed: swap is active and debugAllowSwap waived it (operator
//     opt-out). Only reported when a waiver actually mattered.
//   - SwapOverridden: no swap area was reported by the REUSABLE_CI_PROC_SWAPS
//     file rather than by /proc/swaps.
//   - SwapUndetermined: /proc/swaps is unreadable — we can't assert swap is
//     on without positive evidence, and refusing would block legitimate
//     restricted-container deployments where the indeterminacy is
//     structural. The policy did not run; the caller says so.
//
// Returns errs.ErrInvalidConfig (exit code EX_CONFIG = 78) when
// swap is active and debugAllowSwap is false.
func RequireNoSwap(debugAllowSwap bool) (SwapState, error) {
	on, err := SwapEnabled()
	if err != nil {
		// Deliberate fail-open: when /proc/swaps isn't readable
		// (containerless test runs, locked-down jail, /proc not mounted)
		// we can't determine swap state. The defensive posture would be
		// to block, but that would refuse to run a great many legitimate
		// CI setups for a check that is a defence-in-depth layer. Nothing
		// else keeps key material out of swap: the process hardening
		// (RLIMIT_CORE, PR_SET_DUMPABLE) covers core files and debuggers, and
		// no memory is mlocked. Adopters in strict-posture environments can
		// surface the underlying error via a separate `SwapEnabled()`
		// call if they want hard guarantees.
		return SwapUndetermined, nil //nolint:nilerr // see comment above
	}

	if !on {
		if os.Getenv(procSwapsOverrideEnv) != "" {
			return SwapOverridden, nil
		}

		return SwapAbsent, nil
	}

	// Swap is active. The operator's explicit waiver is consulted only now,
	// so --debug-allow-swap on a swap-free runner is not reported as a
	// bypass: nothing was waived, and a loud warning there would train
	// operators to ignore the one that matters.
	if debugAllowSwap {
		return SwapBypassed, nil
	}

	return SwapAbsent, fmt.Errorf(
		"swap is enabled on this runner; refusing to handle private-key material to prevent swap-page extraction.\n\n"+
			"Fix the runner:\n"+
			"  • disable swap: `sudo swapoff -a` (persist via /etc/fstab)\n"+
			"  • OR run release-signing on a hosted runner (GHA-hosted has swap off by default)\n\n"+
			"Or use reusable-ci release sign with a backend that does not load a long-lived private key into this process:\n"+
			"  --method=sigstore: Sigstore keyless via cosign and OIDC (ephemeral keys are still handled by cosign)\n"+
			"  --method=kms: KMS-backed signing with a remote key reference (a local key file is not remote KMS)\n"+
			"  Backend credentials and subprocess key handling still require a trusted runner.\n\n"+
			"Debug-only escape hatch (LOCAL USE ONLY — NOT for production releases):\n"+
			"  reusable-ci release sign --debug-allow-swap\n"+
			"  When set, the policy is bypassed and a Warning annotation is emitted.\n\n"+
			"Background: docs/verification.md#swap-policy: %w",
		errs.ErrInvalidConfig,
	)
}
