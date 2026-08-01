// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Command reusable-ci is the CLI invoked by every reusable workflow
// in this repository. One Go binary, one CLI surface: validators,
// builders, publishers, signers, SBOM generators, and step-summary
// writers all live here so the workflow YAML stays declarative and
// the testable logic stays in Go.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/diggsweden/reusable-ci/v3/internal/cli"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
)

// bugReportURL is the public issue tracker — quoted to operators on
// panic per clig.dev §Errors ("make it effortless to submit bug
// reports"). Kept as a package-level constant rather than reaching for
// it from cli.Description so the panic path stays self-contained.
const bugReportURL = "https://github.com/diggsweden/reusable-ci/v3/issues/new"

// Build metadata, injected via -ldflags at link time. See justfile / .goreleaser.yml.
//
//nolint:gochecknoglobals // ldflags can only target package vars.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// exitInterrupted is the conventional POSIX exit code for a process
// terminated by SIGINT (128 + 2). Used only by the force-quit path of
// watchSignals — normal context-canceled returns flow through
// errs.ExitCodeFromError.
const exitInterrupted = 130

// osExit is a test seam: production code goes straight to os.Exit, but
// signals_test.go swaps this so the force-quit path can be exercised
// without terminating the test process.
//
//nolint:gochecknoglobals // test seam needs package-var indirection.
var osExit = os.Exit

func main() {
	// Process-level hardening: disable core dumps and ptrace exposure.
	// Linux-only; no-op elsewhere. Best-effort — failures are
	// intentionally silent (logging would leak runner config). Must run
	// before any subcommand brings sensitive material into memory.
	safeexec.HardenProcess()

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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// SIGINT/SIGTERM handling per clig.dev §Signals:
	//   1. First signal cancels ctx (propagates to subprocesses via
	//      exec.CommandContext) and tells the user what just happened.
	//   2. Second signal hard-exits, in case in-flight cleanup hangs.
	// A buffered channel of size 2 means the kernel can deliver both
	// signals without dropping; the goroutine reads them in order.
	sigCh := make(chan os.Signal, 2)

	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go watchSignals(ctx, sigCh, cancel, os.Stderr)

	cmd := cli.New(cli.BuildInfo{
		Version: version,
		Commit:  commit,
		Date:    date,
	})

	err := cmd.Run(ctx, os.Args)
	if err == nil {
		return int(errs.ExitCodeOK)
	}

	// Detect framework-level errors that urfave/cli has already printed
	// to its ErrWriter (e.g. "Incorrect Usage: flag provided but not
	// defined") before we re-print and double up. Capture this before
	// ClassifyError rewrites the error chain.
	alreadyPrinted := cli.IsAlreadyPrintedByCLI(err)

	// cli.ClassifyError wraps urfave/cli's framework-level errors
	// (required-flag-missing, unknown subcommand, unknown flag) with
	// errs.ErrUsage so ExitCodeFromError below maps them to exit 2
	// instead of the default Software/70. Without this, an operator
	// typo would surface as "internal bug" — wrong category, hard to
	// script against.
	err = cli.ClassifyError(err)

	// errs.ExitCodeFromError handles the small set of recognised
	// shapes (nil → OK, context.Canceled → Usage, ErrUnsupported →
	// Unavailable); everything else maps to Software.
	exitCode := int(errs.ExitCodeFromError(err))

	// Default stderr surface is the urfave/cli-style "Error: …" line
	// alone — no timestamp, no level label (see clig.dev §Output:
	// "Don't treat stderr like a log file"). The structured record is
	// kept for --log-level debug where operators want machine-parseable
	// failure data.
	slog.Debug("command failed", "err", err, "exit_code", exitCode)

	if !alreadyPrinted {
		_, _ = fmt.Fprintln(os.Stderr, "Error:", err)
	}

	return exitCode
}

// watchSignals implements the two-stage Ctrl-C contract: first signal
// announces the interrupt and cancels ctx so in-flight work can unwind
// gracefully; second signal force-quits with exitInterrupted.
//
// If ctx ends from another cause (normal completion, defer cancel()),
// the goroutine returns silently — no message, no exit.
func watchSignals(ctx context.Context, sigCh <-chan os.Signal, cancel context.CancelFunc, stderr io.Writer) {
	// First select: wait for either a signal or programmatic ctx end
	// (normal completion path).
	select {
	case <-sigCh:
		_, _ = fmt.Fprintln(stderr, "\n^C interrupted; press Ctrl-C again to force-quit.")

		cancel()
	case <-ctx.Done():
		return
	}
	// Force-quit path is now armed. Wait for a second signal.
	// If main returns first (graceful unwind succeeded), the process
	// exits and this goroutine is reaped along with it — leaking it
	// here is intentional, matching the "fire and forget" lifetime of
	// the signal watcher.
	<-sigCh

	_, _ = fmt.Fprintln(stderr, "Force-quit.")

	osExit(exitInterrupted)
}

// recoverPanic is main's panic boundary. Any panic that escapes run()
// is caught here, printed with a stack trace and a bug-report invitation,
// and converted to an ExitCodeSoftware (70) exit. Without this, the Go
// runtime's default panic handler prints the stack and exits with code 2
// — which CI can't distinguish from a normal "usage error" exit.
func recoverPanic() {
	r := recover()
	if r == nil {
		return
	}

	formatPanic(os.Stderr, r, debug.Stack(), version, commit, os.Args)
	os.Exit(int(errs.ExitCodeSoftware))
}

// formatPanic renders the panic message, stack trace, diagnostic
// context, and bug-report URL to w. Split out from recoverPanic so the
// rendering is testable without invoking os.Exit. The URL is the final
// line so the operator's eye lands on the actionable bit (clig.dev
// §Errors: "important info at the end").
func formatPanic(w io.Writer, panicValue any, stack []byte, vsn, sha string, args []string) {
	_, _ = fmt.Fprintf(w, "reusable-ci: internal error: %v\n\n%s\n", panicValue, stack)
	_, _ = fmt.Fprintf(w, "Context: version=%s  commit=%s  command=%s\n\n", vsn, sha, strings.Join(args, " "))
	_, _ = fmt.Fprintln(w, "This is a bug in reusable-ci — please report it (the link pre-fills the details above):")
	_, _ = fmt.Fprintln(w, "  "+bugReportLink(panicValue, vsn, sha, args))
}

// bugReportLink builds a GitHub "new issue" URL pre-populated with the
// panic summary (title) and the environment (body), so filing a crash
// report is one click plus pasting the stack trace — clig.dev §Errors:
// "provide a URL and have it pre-populate as much information as
// possible." The full panic value + stack still print to the terminal,
// so the title is truncated to keep the URL manageable.
func bugReportLink(panicValue any, vsn, sha string, args []string) string {
	title := fmt.Sprintf("panic: %v", panicValue)
	if len(title) > 120 {
		title = title[:117] + "..."
	}

	body := fmt.Sprintf(
		"Environment:\n- version: %s\n- commit: %s\n- command: %s\n\nStack trace (paste from the terminal output above):\n",
		vsn, sha, strings.Join(args, " "))

	query := url.Values{"title": {title}, "body": {body}}

	return bugReportURL + "?" + query.Encode()
}
