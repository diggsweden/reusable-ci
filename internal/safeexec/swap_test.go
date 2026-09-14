// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build linux

package safeexec_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
)

// writeProcSwaps creates a fake /proc/swaps body and points the kernel reader
// at it, with the override variable cleared so the result is kernel state.
func writeProcSwaps(t *testing.T, body string) {
	t.Helper()

	t.Setenv("REUSABLE_CI_PROC_SWAPS", "")
	safeexec.UseProcSwapsFixture(t, writeSwapsFile(t, body))
}

func writeSwapsFile(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "swaps")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestSwapEnabled_HeaderOnlyIsFalse(t *testing.T) {
	writeProcSwaps(t, "Filename\tType\tSize\tUsed\tPriority\n")

	on, err := safeexec.SwapEnabled()
	if err != nil {
		t.Fatal(err)
	}

	if on {
		t.Errorf("header-only /proc/swaps must report swap off")
	}
}

func TestSwapEnabled_ActiveSwapAreaIsTrue(t *testing.T) {
	writeProcSwaps(t, `Filename	Type	Size	Used	Priority
/dev/sda2	partition	2097148	0	-2
`)

	on, err := safeexec.SwapEnabled()
	if err != nil {
		t.Fatal(err)
	}

	if !on {
		t.Errorf("active swap area must report swap on")
	}
}

func TestSwapEnabled_ReadErrorIsIndeterminate(t *testing.T) {
	t.Setenv("REUSABLE_CI_PROC_SWAPS", "")
	safeexec.UseProcSwapsFixture(t, filepath.Join(t.TempDir(), "does-not-exist"))

	on, err := safeexec.SwapEnabled()
	if err == nil {
		t.Fatal("expected non-nil error on missing /proc/swaps")
	}

	if on {
		t.Errorf("indeterminate state must NOT claim swap on")
	}
}

func TestRequireNoSwap_RefusesOnSwapOn(t *testing.T) {
	writeProcSwaps(t, `Filename	Type	Size	Used	Priority
/dev/sda2	partition	2097148	0	-2
`)

	state, err := safeexec.RequireNoSwap(false)
	if err == nil {
		t.Fatal("expected refusal on swap-enabled host")
	}

	if state != safeexec.SwapAbsent {
		t.Errorf("a refusal must not report a passing state; got %s", state)
	}

	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig (exit EX_CONFIG=78), got %v", err)
	}

	for _, want := range []string{"swapoff", "hosted runner", "--method=sigstore", "--method=kms", "ephemeral keys", "local key file is not remote KMS", "--debug-allow-swap"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message must mention %q for operator guidance; got:\n%s", want, err.Error())
		}
	}

	if strings.Contains(err.Error(), "does not yet provide") || strings.Contains(err.Error(), "outside reusable-ci") {
		t.Fatalf("stale backend guidance: %v", err)
	}
}

func TestRequireNoSwap_PassesWhenSwapOff(t *testing.T) {
	writeProcSwaps(t, "Filename\tType\tSize\tUsed\tPriority\n")

	state, err := safeexec.RequireNoSwap(false)
	if err != nil {
		t.Errorf("expected nil on swap-off host, got %v", err)
	}

	// SwapAbsent, not SwapUndetermined: the file was read and reported no
	// swap area. The caller stays silent only for this state, so conflating
	// it with "we could not look" would put a notice on every clean run.
	if state != safeexec.SwapAbsent {
		t.Errorf("swap-off host reported %s, want absent", state)
	}
}

func TestRequireNoSwap_SoftPassesOnReadError(t *testing.T) {
	// Indeterminate state — refusing here would block legitimate
	// restricted-container deployments where /proc/swaps is hidden.
	t.Setenv("REUSABLE_CI_PROC_SWAPS", "")
	safeexec.UseProcSwapsFixture(t, filepath.Join(t.TempDir(), "does-not-exist"))

	state, err := safeexec.RequireNoSwap(false)
	if err != nil {
		t.Errorf("expected soft-pass on indeterminate state, got %v", err)
	}

	// The soft-pass must be distinguishable from a clean run: this is the
	// bypass the operator did not choose, and the CLI turns this state into
	// the Notice that says the policy did not run.
	if state != safeexec.SwapUndetermined {
		t.Errorf("unreadable /proc/swaps reported %s, want undetermined — the skip would pass silently", state)
	}
}

// TestRequireNoSwap_DebugOverridePasses pins the documented escape
// hatch: when the caller passes debugAllowSwap=true (sourced from
// the --debug-allow-swap CLI flag), the policy passes.
func TestRequireNoSwap_DebugOverridePasses(t *testing.T) {
	writeProcSwaps(t, `Filename	Type	Size	Used	Priority
/dev/sda2	partition	2097148	0	-2
`)

	state, err := safeexec.RequireNoSwap(true)
	if err != nil {
		t.Errorf("debug override must bypass refusal, got %v", err)
	}

	if state != safeexec.SwapBypassed {
		t.Errorf("waived an active swap area but reported %s, want bypassed", state)
	}
}

// TestRequireNoSwap_DebugOverrideOnSwapFreeHostIsNotABypass: --debug-allow-swap
// on a host with no swap waives nothing, so it must not be reported as a
// bypass. The caller renders SwapBypassed as a loud "DO NOT USE FOR PRODUCTION"
// warning; emitting that when the policy was satisfied on its own terms trains
// operators to ignore the warning that matters.
func TestRequireNoSwap_DebugOverrideOnSwapFreeHostIsNotABypass(t *testing.T) {
	writeProcSwaps(t, "Filename\tType\tSize\tUsed\tPriority\n")

	state, err := safeexec.RequireNoSwap(true)
	if err != nil {
		t.Fatalf("swap-off host with the override set must pass, got %v", err)
	}

	if state != safeexec.SwapAbsent {
		t.Errorf("override on a swap-free host reported %s, want absent (nothing was waived)", state)
	}
}

// TestSwapState_NamesEveryState pins the label each state renders.
//
// The labels are what an operator reads to tell three very different outcomes
// apart: swap was checked and is off, swap could not be checked at all, and
// swap is on but was deliberately waived. Only the middle one is a silent
// bypass nobody chose, so conflating it with either neighbour hides exactly
// the case worth surfacing. Nothing pinned them, and collapsing the switch to
// a single return left every swap test passing — none of them reads a label.
func TestSwapState_NamesEveryState(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		state safeexec.SwapState
		want  string
	}{
		{safeexec.SwapAbsent, "absent"},
		{safeexec.SwapUndetermined, "undetermined"},
		{safeexec.SwapBypassed, "bypassed"},
		{safeexec.SwapOverridden, "overridden"},
	} {
		if got := tc.state.String(); got != tc.want {
			t.Errorf("SwapState(%d).String() = %q, want %q", tc.state, got, tc.want)
		}
	}

	// Distinctness is the property the labels exist for; equal-looking
	// labels would satisfy the table above only if it were written wrong,
	// but a future state added without a case would render "unknown" and
	// silently share it with any other unnamed state.
	seen := map[string]bool{}

	for _, state := range []safeexec.SwapState{safeexec.SwapAbsent, safeexec.SwapUndetermined, safeexec.SwapBypassed, safeexec.SwapOverridden} {
		label := state.String()
		if seen[label] {
			t.Errorf("two states render the same label %q", label)
		}

		seen[label] = true
	}

	// An unnamed state must be recognisable as such rather than borrowing a
	// real state's label.
	if got := safeexec.SwapState(99).String(); got != "unknown" {
		t.Errorf("unnamed state rendered %q, want %q", got, "unknown")
	}
}

// TestRequireNoSwap_OverrideIsReportedNotTakenAsKernelState sets
// REUSABLE_CI_PROC_SWAPS while the kernel reader points at a file with an
// active swap area. The override decides the answer, as the black-box suite
// needs, but a pass read from it is SwapOverridden rather than SwapAbsent, so
// fixture bytes are never reported as measured kernel state. An override that
// reports swap still refuses, and an unreadable override is undetermined.
func TestRequireNoSwap_OverrideIsReportedNotTakenAsKernelState(t *testing.T) {
	kernelOn := writeSwapsFile(t, "Filename\tType\tSize\tUsed\tPriority\n/dev/sda2\tpartition\t2097148\t0\t-2\n")

	for _, tc := range []struct {
		name      string
		override  string
		wantState safeexec.SwapState
		wantErr   error
	}{
		{"override reports no swap", writeSwapsFile(t, "Filename\tType\tSize\tUsed\tPriority\n"), safeexec.SwapOverridden, nil},
		{"empty override file", writeSwapsFile(t, ""), safeexec.SwapOverridden, nil},
		{"override reports swap", kernelOn, safeexec.SwapAbsent, errs.ErrInvalidConfig},
		{"unreadable override", filepath.Join(t.TempDir(), "missing"), safeexec.SwapUndetermined, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			safeexec.UseProcSwapsFixture(t, kernelOn)
			t.Setenv("REUSABLE_CI_PROC_SWAPS", tc.override)

			state, err := safeexec.RequireNoSwap(false)
			if state != tc.wantState || !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("state = %s, err = %v; want %s, %v", state, err, tc.wantState, tc.wantErr)
			}
		})
	}
}
