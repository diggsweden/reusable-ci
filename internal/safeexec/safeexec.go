// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package safeexec wraps os/exec for the case where the binary path is
// a trusted/controlled value — a hardcoded default tool name like
// "git", an adapter field set by the CLI binding (e.g. trivy.Bin),
// or a path the operator passed via a typed CLI flag.
//
// The single //nolint:gosec G204 directive lives here so every adapter
// call site can drop its own. Callers must keep the contract: never
// pass a binary path derived directly from untrusted input (user-typed
// raw strings, headers, env values the user can inject) — only typed
// adapter fields, package constants, or pre-validated paths.
package safeexec

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// cancelGrace bounds how long Wait blocks after the context is cancelled
// (Ctrl-C, timeout) or the process exits with I/O still open. exec sends
// SIGKILL to the direct child immediately on cancel, but a grandchild
// (e.g. git-remote-https stuck in connect) can inherit the stdout/stderr
// pipe and keep Wait blocking forever. WaitDelay caps that: after the
// grace, exec force-closes the pipes and kills the process so Wait
// returns and the command is cancellable. Generous enough not to truncate
// a slow legitimate flush; a second Ctrl-C force-quits instantly anyway.
const cancelGrace = 10 * time.Second

// Command builds an *exec.Cmd for ctx using a trusted binary path and
// typed arguments. Equivalent to exec.CommandContext with the gosec
// G204 audit concentrated to this single site.
//
// Read the package doc before adding new call sites.
// The whole purpose of this package is to concentrate the
// non-static-command audit at this one site. Callers must pass a
// trusted/controlled bin per the package doc; the contract is
// upheld by the adapter layer.
func Command(ctx context.Context, bin string, args ...string) *exec.Cmd {
	//nolint:gosec // by package contract: bin is trusted/controlled — see package doc.
	cmd := exec.CommandContext(ctx, bin, args...) // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd.WaitDelay = cancelGrace

	return cmd
}

// WrapError classifies a subprocess error from cmd.Run / cmd.CombinedOutput
// into the project's sentinel ladder so callers don't have to spell
// the policy out at every adapter:
//
//   - nil                    → nil (passthrough)
//   - exec.ErrNotFound       → ErrDependencyUnavailable (exit 69)
//     "operator forgot to install the toolchain"
//   - any other error        → ErrValidation (exit 1)
//     "the work the user asked for failed"
//
// bin is the trusted binary name. action is a short label (e.g. the
// leading positional arg "test" / "build" / "publish") that goes into
// the message — pass "" to omit and just blame the binary. The
// subprocess's own stdout/stderr is already streamed to the caller,
// so this helper deliberately does NOT dump the full argv into the
// wrap.
func WrapError(err error, bin, action string) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%s not found in $PATH: %w", bin, errs.ErrDependencyUnavailable)
	}

	if action == "" {
		return fmt.Errorf("%s: %w: %w", bin, err, errs.ErrValidation)
	}

	return fmt.Errorf("%s %s: %w: %w", bin, action, err, errs.ErrValidation)
}

// FirstArg returns the leading positional argument, or "" when args is
// empty. It is the conventional source of the `action` label passed to
// WrapError (the subcommand verb: "test" / "build" / "publish"), shared
// by every exec adapter so the extraction lives in one place.
func FirstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}

	return args[0]
}
