// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

//go:build linux

package safeexec_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/safeexec"
)

// writeProcSwaps creates a fake /proc/swaps body and points the
// SwapEnabled reader at it via REUSABLE_CI_PROC_SWAPS.
func writeProcSwaps(t *testing.T, body string) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "swaps")

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("REUSABLE_CI_PROC_SWAPS", path)
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
	t.Setenv("REUSABLE_CI_PROC_SWAPS", filepath.Join(t.TempDir(), "does-not-exist"))

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

	err := safeexec.RequireNoSwap(false)
	if err == nil {
		t.Fatal("expected refusal on swap-enabled host")
	}

	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig (exit EX_CONFIG=78), got %v", err)
	}

	for _, want := range []string{"swapoff", "hosted runner", "Sigstore", "KMS", "--debug-allow-swap"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message must mention %q for operator guidance; got:\n%s", want, err.Error())
		}
	}
}

func TestRequireNoSwap_PassesWhenSwapOff(t *testing.T) {
	writeProcSwaps(t, "Filename\tType\tSize\tUsed\tPriority\n")

	if err := safeexec.RequireNoSwap(false); err != nil {
		t.Errorf("expected nil on swap-off host, got %v", err)
	}
}

func TestRequireNoSwap_SoftPassesOnReadError(t *testing.T) {
	// Indeterminate state — refusing here would block legitimate
	// restricted-container deployments where /proc/swaps is hidden.
	t.Setenv("REUSABLE_CI_PROC_SWAPS", filepath.Join(t.TempDir(), "does-not-exist"))

	if err := safeexec.RequireNoSwap(false); err != nil {
		t.Errorf("expected soft-pass on indeterminate state, got %v", err)
	}
}

// TestRequireNoSwap_DebugOverridePasses pins the documented escape
// hatch: when the caller passes debugAllowSwap=true (sourced from
// the --debug-allow-swap CLI flag), the policy passes.
func TestRequireNoSwap_DebugOverridePasses(t *testing.T) {
	writeProcSwaps(t, `Filename	Type	Size	Used	Priority
/dev/sda2	partition	2097148	0	-2
`)

	if err := safeexec.RequireNoSwap(true); err != nil {
		t.Errorf("debug override must bypass refusal, got %v", err)
	}
}

