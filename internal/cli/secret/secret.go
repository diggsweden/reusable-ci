// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package secret resolves secrets from a file path or environment
// variable. Secrets must never reach argv, so CLI subcommands accept
// them via a `--<name>-file` flag (with `-` reading from stdin) or via
// an environment variable, and call [Resolve] to pick the value up.
package secret

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/cliio"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
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

func readFile(path string) (string, error) {
	if path == "-" {
		// Refuse to read from a character device (TTY or /dev/null) —
		// clig.dev says commands expecting piped stdin should not hang
		// on a TTY. Stat-based detection treats /dev/null the same as
		// a TTY; either way the value would be empty, so the error
		// here is more informative than the downstream "required"
		// error the empty value would trigger.
		if cliio.StdinIsCharDevice() {
			return "", fmt.Errorf("%q expects piped or redirected input, not a terminal: %w", path, errs.ErrUsage)
		}

		body, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read secret from stdin: %w", err)
		}

		return strings.TrimRight(string(body), "\r\n"), nil
	}

	body, err := os.ReadFile(path) //nolint:gosec // path is the --*-file CLI flag — operator-supplied secret path.
	if err != nil {
		return "", fmt.Errorf("read secret from %q: %w", path, err)
	}

	return strings.TrimRight(string(body), "\r\n"), nil
}
