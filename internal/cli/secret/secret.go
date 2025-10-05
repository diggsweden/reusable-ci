// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package secret resolves secrets from a file path or environment
// variable. Secrets must never reach argv, so CLI subcommands accept
// them via a `--<name>-file` flag (with `-` reading from stdin) or via
// an environment variable, and call [Resolve] to pick the value up.
package secret

import (
	"fmt"
	"os"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
)

// Resolve loads a secret value, preferring a file path (with `-` reading
// from stdin) over an environment variable. Trailing CR/LF bytes are
// trimmed so values saved with a trailing newline (common from `echo`,
// heredocs, etc.) don't poison downstream consumers.
//
// Returns "" with no error when neither source has a value — callers
// decide whether the secret is required for their use case and surface
// a domain-specific error message in that case.
func Resolve(filePath, envVar string) (string, error) {
	if filePath != "" {
		return readFile(filePath)
	}

	if envVar == "" {
		return "", nil
	}

	return strings.TrimRight(os.Getenv(envVar), "\r\n"), nil
}

// readFile loads the secret through cliio.ReadFile, which owns three things
// this package should not re-implement: the "-" stdin sentinel with its
// character-device guard (clig.dev — a command expecting piped stdin must not
// hang on a TTY), a bound on how much is read, and the sentinel classification
// that decides the exit code. A missing --*-file path is the operator's
// mistake (EX_NOINPUT) and an unreadable one is a permission problem
// (EX_NOPERM); reading it here with a bare os.ReadFile left both unclassified,
// so they exited EX_SOFTWARE (70) — "file a bug" — for a mistyped path.
func readFile(path string) (string, error) {
	body, err := cliio.ReadFile(path)
	if err != nil {
		if path == cliio.StdSentinel {
			return "", fmt.Errorf("read secret from stdin: %w", err)
		}

		return "", fmt.Errorf("read secret from %q: %w", path, err)
	}

	return strings.TrimRight(string(body), "\r\n"), nil
}
