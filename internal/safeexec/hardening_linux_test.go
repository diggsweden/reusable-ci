// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build linux

package safeexec_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

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

	parentLimit, parentDumpable := processHardeningState(t)

	ctx, cancel := context.WithTimeout(t.Context(), hardenChildTimeout)
	defer cancel()

	// The child is selected by an anchored name and gets only its marker in
	// the environment, so neither a sibling test nor an inherited variable
	// decides what it runs.
	//nolint:gosec // os.Args[0] is this test binary.
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHardenProcess_DisablesCoreDumpsAndPtrace$", "-test.v", "-test.count=1")
	cmd.Env = []string{hardenChildEnv + "=1"}

	out, err := cmd.CombinedOutput()

	// The two processes share nothing, but say so: the parent keeps its own
	// limits whatever the child did.
	if limit, dumpable := processHardeningState(t); limit != parentLimit || dumpable != parentDumpable {
		t.Errorf("parent state changed: RLIMIT_CORE %+v -> %+v, dumpable %d -> %d", parentLimit, limit, parentDumpable, dumpable)
	}

	outcome, reason := hardenChildOutcome(string(out))

	switch {
	case outcome == "skip":
		// An unavailable premise is not a hardening failure, and it is not
		// proof either: the test is skipped with the child's reason.
		t.Skipf("hardening premise unavailable in this environment: %s", reason)
	case err != nil || outcome != "pass":
		t.Fatalf("hardening child did not pass its assertions (err %v):\n%s", err, out)
	}
}

// hardenChildTimeout bounds the re-exec so a wedged child cannot hold the run.
const hardenChildTimeout = 30 * time.Second

// hardenChildOutcome reads the child's verdict for this test by name: "pass",
// "skip" with the skip reason, or "" when it neither passed nor skipped. A bare
// PASS at the end of the output does not count; a child that skipped ends
// with PASS too.
func hardenChildOutcome(out string) (string, string) {
	const name = "TestHardenProcess_DisablesCoreDumpsAndPtrace"

	lines := strings.Split(out, "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "--- PASS: "+name+" "):
			return "pass", ""
		case strings.HasPrefix(line, "--- SKIP: "+name+" "):
			if i+1 < len(lines) {
				return "skip", strings.TrimSpace(lines[i+1])
			}

			return "skip", ""
		}
	}

	return "", ""
}

func processHardeningState(t *testing.T) (unix.Rlimit, int) {
	t.Helper()

	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_CORE, &limit); err != nil {
		t.Fatalf("getrlimit: %v", err)
	}

	return limit, getDumpable(t)
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

// TestHardenChildOutcome_OnlyANamedPassIsProof pins how the parent reads the
// child: a named pass is proof, a named skip carries its reason and is not,
// and a trailing PASS, a failure or another test's pass are not proof.
func TestHardenChildOutcome_OnlyANamedPassIsProof(t *testing.T) {
	t.Parallel()

	const name = "TestHardenProcess_DisablesCoreDumpsAndPtrace"

	for _, tc := range []struct {
		out, outcome, reason string
	}{
		{"=== RUN   " + name + "\n--- PASS: " + name + " (0.00s)\nPASS\n", "pass", ""},
		{"=== RUN   " + name + "\n--- SKIP: " + name + " (0.00s)\n    hardening_linux_test.go:1: RLIMIT_CORE is pinned at 0\nPASS\n", "skip", "hardening_linux_test.go:1: RLIMIT_CORE is pinned at 0"},
		{"testing: warning: no tests to run\nPASS\n", "", ""},
		{"--- FAIL: " + name + " (0.00s)\nFAIL\n", "", ""},
		{"--- PASS: " + name + "Extra (0.00s)\nPASS\n", "", ""},
	} {
		if outcome, reason := hardenChildOutcome(tc.out); outcome != tc.outcome || reason != tc.reason {
			t.Errorf("hardenChildOutcome(%q) = (%q, %q), want (%q, %q)", tc.out, outcome, reason, tc.outcome, tc.reason)
		}
	}
}
