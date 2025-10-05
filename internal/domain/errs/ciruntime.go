// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package errs

import (
	"errors"
	"fmt"
	"strings"
)

// ErrCIRuntimeRequired marks a command that needs the CI runner's injected
// context (forge, repository, run, per-job token) but was run without it —
// typically at a local terminal. Maps to ExitCodeUsage. Build instances with
// RuntimeRequired so the message lists the missing variables and explains why
// there is no flag for them.
//
// This is the package's one structured error: the multi-line, variable-listing
// message is the concrete demand the errs.go header anticipates ("add a typed
// builder when a real requirement appears"). Everything else stays a sentinel.
var ErrCIRuntimeRequired = errors.New("requires a CI job runtime")

// EnvVar names one runner-injected variable, for RuntimeRequired's listing.
type EnvVar struct {
	Name string
	What string
}

type ciRuntimeError struct{ msg string }

func (e *ciRuntimeError) Error() string { return e.msg }

func (e *ciRuntimeError) Is(target error) bool { return target == ErrCIRuntimeRequired }

// RuntimeRequired builds the friendly error for a command that depends on the
// CI runner's injected context but was run without it. op names the operation
// (e.g. "transfer run artifacts"); provider is the detected forge ("" → "none").
// The returned error satisfies errors.Is(err, ErrCIRuntimeRequired).
func RuntimeRequired(op, provider string, need []EnvVar) error {
	if provider == "" {
		provider = "none"
	}

	var buf strings.Builder

	fmt.Fprintf(&buf, "cannot %s outside a CI job.\n\n", op)
	buf.WriteString("The forge, repository, run and credentials are read from the job ")
	buf.WriteString("environment.\nThere is no --token flag: the runner token is minted per-job ")
	buf.WriteString("and cannot\nbe supplied by hand. ")
	fmt.Fprintf(&buf, "Detected provider: %s.\n\n", provider)
	// Provider-neutral prose; the var NAMES are forge-specific and supplied by
	// the caller (the adapter) — e.g. ACTIONS_RUNTIME_TOKEN on GitHub/Forgejo
	// Actions, CI_JOB_TOKEN on GitLab. They are the runner's variables, not ours.
	buf.WriteString("Required (provided by the CI runner inside a job — not reusable-ci variables):\n")

	for _, v := range need {
		fmt.Fprintf(&buf, "   %-21s %s\n", v.Name, v.What)
	}

	return &ciRuntimeError{msg: strings.TrimRight(buf.String(), "\n")}
}

// Credential names one user-suppliable secret and how to provide it, for
// CredentialRequired. Flag is the bare --x-file name (empty when the secret is
// env-only); Env is the fallback variable.
type Credential struct {
	What string // human name, e.g. "release-bot token"
	Flag string // the --x-file flag name, e.g. "token-file" ("" = env-only)
	Env  string // the env var, e.g. "RELEASE_TOKEN"
}

// CredentialRequired builds the uniform "X is required: pass --x-file (or set
// $X)" error for one or more user-suppliable secrets that are missing. Unlike
// RuntimeRequired (runner-injected, no flag), these CAN be supplied — so the
// message points at the flag/env that provides each. Wraps ErrUsage.
//
// Secrets are passed via file/stdin or env, never an argv value flag, so the
// flag form is always `--<flag> <path>` ("-" for stdin); CredentialRequired
// renders exactly that.
func CredentialRequired(creds ...Credential) error {
	var buf strings.Builder

	for i, cred := range creds {
		if i > 0 {
			buf.WriteString("; ")
		}

		fmt.Fprintf(&buf, "the %s is required: ", cred.What)

		if cred.Flag != "" {
			fmt.Fprintf(&buf, "pass --%s <path> (\"-\" for stdin) or set $%s", cred.Flag, cred.Env)
		} else {
			fmt.Fprintf(&buf, "set $%s", cred.Env)
		}
	}

	return fmt.Errorf("%s: %w", buf.String(), ErrUsage)
}
