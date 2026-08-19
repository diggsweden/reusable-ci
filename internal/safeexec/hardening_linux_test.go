// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build linux

package safeexec_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
)

// HardenProcess keeps a decrypted signing key from reaching disk or
// another process: no core dump on a crash, no ptrace attach to read
// /proc/<pid>/mem. Both are process-wide and irreversible for the
// process that applies them, so the checks run in a re-exec of this
// test binary rather than in the shared test process -- otherwise every
// later test in the package would inherit a non-dumpable process.
//
// The child asserts the before state as well as the after, so a
// HardenProcess that did nothing on a runner where the limits happened
// to be zero already cannot pass.
const hardenChildEnv = "REUSABLE_CI_TEST_HARDEN_CHILD"

func TestHardenProcess_DisablesCoreDumpsAndPtrace(t *testing.T) {
	t.Parallel()

	if os.Getenv(hardenChildEnv) == "1" {
		runHardenChild(t)

		return
	}

	//nolint:gosec // os.Args[0] is this test binary.
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=TestHardenProcess_DisablesCoreDumpsAndPtrace", "-test.v")

	cmd.Env = append(os.Environ(), hardenChildEnv+"=1")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hardening child failed: %v\n%s", err, out)
	}

	// Deliberately not a bare "PASS": a child that skipped every
	// assertion still ends its output with PASS, and a skip here means
	// the environment made the before/after states indistinguishable --
	// which is exactly the case this test must not silently accept.
	if !strings.Contains(string(out), "--- PASS: TestHardenProcess") {
		t.Fatalf("hardening child did not run its assertions:\n%s", out)
	}
}

// runHardenChild is the body that executes in the re-exec'd process.
func runHardenChild(t *testing.T) {
	t.Helper()

	// A core dump has to be possible to begin with, or "no core dump
	// afterwards" says nothing. The runner's inherited limit may be
	// anything, so raise it to whatever the hard limit allows.
	var before unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_CORE, &before); err != nil {
		t.Fatalf("getrlimit: %v", err)
	}

	if err := unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{Cur: before.Max, Max: before.Max}); err != nil {
		t.Skipf("cannot raise RLIMIT_CORE in this environment: %v", err)
	}

	var raised unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_CORE, &raised); err != nil {
		t.Fatalf("getrlimit: %v", err)
	}

	if raised.Cur == 0 {
		t.Skip("RLIMIT_CORE is pinned at 0 by the environment; the after state would be indistinguishable")
	}

	if dumpable := getDumpable(t); dumpable != 1 {
		t.Skipf("process starts non-dumpable (%d); the after state would be indistinguishable", dumpable)
	}

	safeexec.HardenProcess()

	var after unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_CORE, &after); err != nil {
		t.Fatalf("getrlimit: %v", err)
	}

	// The hard limit matters as much as the soft one: a non-zero hard
	// limit lets anything in the process raise the soft limit back
	// before crashing.
	if after.Cur != 0 || after.Max != 0 {
		t.Errorf("RLIMIT_CORE = {cur:%d max:%d}, want both 0", after.Cur, after.Max)
	}

	if dumpable := getDumpable(t); dumpable != 0 {
		t.Errorf("PR_GET_DUMPABLE = %d, want 0 (no ptrace attach, no core dump)", dumpable)
	}
}

func getDumpable(t *testing.T) int {
	t.Helper()

	dumpable, err := unix.PrctlRetInt(unix.PR_GET_DUMPABLE, 0, 0, 0, 0)
	if err != nil {
		t.Fatalf("prctl(PR_GET_DUMPABLE): %v", err)
	}

	return dumpable
}
