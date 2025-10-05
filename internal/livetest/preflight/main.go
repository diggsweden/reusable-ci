// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

func main() { //nolint:cyclop // Two strict flag modes share one small provider-free binary.
	outputDir := flag.String("output-dir", "", "private directory to create for frozen live inputs")
	verifyCleanup := flag.Bool("verify-cleanup", false, "verify captured cleanup file identities without provider access")
	runCleanup := flag.Bool("run-cleanup", false, "verify and execute the frozen cleanup launcher")
	cleanupCommand := flag.String("cleanup-command", "", "captured cleanup command path")
	cleanupCommandFacts := flag.String("cleanup-command-facts", "", "captured cleanup command identity and digest")
	cleanupContractFile := flag.String("cleanup-contract-file", "", "captured cleanup contract path")
	cleanupContractFileFacts := flag.String("cleanup-contract-file-facts", "", "captured cleanup contract identity and digest")

	flag.Parse()

	if flag.NArg() != 0 {
		usage()
	}

	if *verifyCleanup || *runCleanup {
		if *outputDir != "" || *cleanupCommand == "" || *cleanupCommandFacts == "" ||
			*cleanupContractFile == "" || *cleanupContractFileFacts == "" || (*verifyCleanup && *runCleanup) {
			usage()
		}

		os.Exit(runCleanupMode(*runCleanup, *cleanupCommand, *cleanupCommandFacts, *cleanupContractFile, *cleanupContractFileFacts))
	}

	if *outputDir == "" || *cleanupCommand != "" || *cleanupCommandFacts != "" ||
		*cleanupContractFile != "" || *cleanupContractFileFacts != "" {
		usage()
	}

	summary, err := livetest.PrepareLiveInputs(*outputDir, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "live preflight: %v\n", err)
		os.Exit(2)
	}

	fmt.Printf("live preflight: %d provider(s), generation %s, namespace %s\n",
		summary.Selected, summary.Generation, livetest.ResourcePrefix)
}

// runCleanupMode verifies, or verifies and launches, the frozen cleanup pair.
// It exits 2 for a refusal before launch and 3 when a launched cleanup failed
// or was stopped at its bound, so the runner can say which happened.
func runCleanupMode(launch bool, command, commandFacts, contractFile, contractFileFacts string) int {
	if !launch {
		if err := livetest.VerifyCleanupBindings(command, commandFacts, contractFile, contractFileFacts); err != nil {
			fmt.Fprintf(os.Stderr, "live cleanup verifier: %v\n", err)

			return 2
		}

		return 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), livetest.FrozenCleanupTimeout)
	defer cancel()

	err := livetest.RunFrozenCleanup(ctx, command, commandFacts, contractFile, contractFileFacts)

	switch {
	case err == nil:
		return 0
	case errors.Is(err, livetest.ErrFrozenCleanupFailed):
		fmt.Fprintf(os.Stderr, "live cleanup launcher: %v\n", err)

		return 3
	default:
		fmt.Fprintf(os.Stderr, "live cleanup verifier: %v\n", err)

		return 2
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: live-preflight --output-dir <absolute-new-directory>")
	fmt.Fprintln(os.Stderr, "   or: live-preflight --verify-cleanup --cleanup-command <path> --cleanup-command-facts <facts> --cleanup-contract-file <path> --cleanup-contract-file-facts <facts>")
	fmt.Fprintln(os.Stderr, "   or: live-preflight --run-cleanup --cleanup-command <path> --cleanup-command-facts <facts> --cleanup-contract-file <path> --cleanup-contract-file-facts <facts>")
	os.Exit(2)
}
