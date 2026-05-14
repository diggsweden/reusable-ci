// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Command reusable-ci is the shared CI/CD logic binary used by the
// diggsweden/reusable-ci workflows. See docs/go-port-plan.md for the
// architecture and migration plan.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/diggsweden/reusable-ci/internal/cli"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// Build metadata, injected via -ldflags at link time. See justfile / .goreleaser.yml.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	defer recoverPanic()

	// recoverPanic catches panics propagating out of run() before os.Exit
	// is reached; the deferred function only runs on the panic path, not
	// on normal exit, so the gocritic exitAfterDefer warning here is a
	// false positive — splitting work into run() guarantees main() calls
	// os.Exit exactly once.
	os.Exit(run()) //nolint:gocritic // exitAfterDefer: defer is the panic boundary
}

// run owns the CLI lifecycle and returns the process exit code. Splitting
// it out of main lets recoverPanic stay deferred — main only calls
// os.Exit once at the bottom of the deferred chain, so the panic
// boundary is never bypassed by an early os.Exit.
func run() int {
	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cmd := cli.New(cli.BuildInfo{
		Version: version,
		Commit:  commit,
		Date:    date,
	})

	err := cmd.Run(ctx, os.Args)
	if err == nil {
		return int(errs.ExitCodeOK)
	}

	// errs.ExitCodeFromError handles the small set of recognised
	// shapes (nil → OK, context.Canceled → Usage, ErrUnsupported →
	// Unavailable); everything else maps to Software.
	exitCode := int(errs.ExitCodeFromError(err))

	slog.Error("command failed", "err", err, "exit_code", exitCode)
	fmt.Fprintln(os.Stderr, "Error:", err)
	return exitCode
}

// recoverPanic is main's panic boundary. Any panic that escapes run()
// is caught here, printed with a stack trace, and converted to an
// ExitCodeSoftware (70) exit. Without this, the Go runtime's default
// panic handler prints the stack and exits with code 2 — which CI
// can't distinguish from a normal "usage error" exit.
func recoverPanic() {
	r := recover()
	if r == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "reusable-ci: internal error: %v\n\n%s\n", r, debug.Stack())
	os.Exit(int(errs.ExitCodeSoftware))
}
