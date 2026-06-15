// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// ClassifyError wraps urfave/cli's framework-level errors with the
// project's typed sentinels so cmd/reusable-ci/main.go's
// errs.ExitCodeFromError maps them to the right sysexits.h exit code.
//
// urfave/cli does not export typed errors for these classes:
//   - "Required flag(s) ... not set"           — plain *errRequiredFlags
//   - "No help topic for '<x>'"                — cli.Exit("...", 3) → an
//     internal *cli.exitError that implements ExitCode() int
//   - "flag provided but not defined: -<x>"    — stdlib flag.Parse error,
//     propagated unwrapped
//
// Without classification all three fall through to ExitCodeSoftware (70),
// which is wrong — they're usage errors. We detect them by message
// prefix and wrap with errs.ErrUsage so they exit 2.
//
// We deliberately do NOT use errors.As(&cli.ExitCoder) here: that
// interface (error + ExitCode() int) is ALSO satisfied by
// *exec.ExitError, which would falsely classify every subprocess
// failure as a usage error. Matching prefixes keeps the rule narrow.
//
// Returns the input unchanged for nil or unrecognized errors.
func ClassifyError(err error) error {
	if err == nil {
		return nil
	}

	msg := rewriteHelpTopicMessage(err.Error())
	if isUsageMessage(msg) {
		return fmt.Errorf("%s: %w", msg, errs.ErrUsage)
	}

	return err
}

// rewriteHelpTopicMessage turns urfave/cli's "No help topic for 'X'"
// into "unknown subcommand 'X'", which more accurately describes what
// the user did (typed a subcommand, not asked for help). Returns the
// input unchanged for unrelated messages.
//
// When --suggest is on, urfave appends its closest-match hint as a bare
// ". <command>" tail (help.go), e.g. "No help topic for 'relese'. release"
// — which reads as noise to the user. We re-label that tail as an
// explicit, clig.dev-style "Did you mean \"release\"?" so the suggestion
// is unmistakable.
func rewriteHelpTopicMessage(msg string) string {
	const prefix = "No help topic for "
	if !strings.HasPrefix(msg, prefix) {
		return msg
	}

	// body is "'<arg>'" or, with a suggestion, "'<arg>'. <suggestion>".
	body := strings.TrimPrefix(msg, prefix)
	if name, suggestion, found := strings.Cut(body, "'. "); found {
		return fmt.Sprintf("unknown subcommand %s'. Did you mean %q?", name, suggestion)
	}

	return "unknown subcommand " + body
}

// IsAlreadyPrintedByCLI reports whether the error has already been
// written to stderr before main.go's print path runs. main.go uses
// this to skip its own `Error: …` line so the user does not see the
// same message twice.
//
// Two sources print before main.go:
//   - urfave/cli auto-prints "Incorrect Usage:" for stdlib flag.Parse
//     failures (unknown flag).
//   - This project's onUsageError hook prints "Error: …" + a `--help`
//     pointer for every flag-parse failure (including required-flag-
//     missing).
//
// Both classes match isUsageMessage, so one check covers them. We do
// NOT treat the "No help topic for" rewrite as already-printed; that
// one only reaches us via the error return and gets the standard
// main.go "Error: …" line.
func IsAlreadyPrintedByCLI(err error) bool {
	if err == nil {
		return false
	}

	msg := err.Error()

	return strings.HasPrefix(msg, "flag provided but not defined") ||
		strings.HasPrefix(msg, "Required flag")
}

// isUsageMessage returns true for the small set of framework-level
// strings urfave/cli emits for user-input errors. Centralised so
// ClassifyError and IsAlreadyPrintedByCLI stay in sync.
func isUsageMessage(msg string) bool {
	switch {
	case strings.HasPrefix(msg, "Required flag"):
		return true
	case strings.HasPrefix(msg, "flag provided but not defined"):
		return true
	case strings.HasPrefix(msg, "No help topic for"):
		return true
	case strings.HasPrefix(msg, "unknown subcommand"):
		return true
	}

	return false
}
