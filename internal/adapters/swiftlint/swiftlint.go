// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package swiftlint wraps the system `swiftlint` binary.
// Installed via `brew install swiftlint` in the workflow; the binary is
// only available on macOS runners.
package swiftlint

import (
	"bytes"
	"context"
	"errors"
	"github.com/diggsweden/reusable-ci/internal/safeexec"
	"os/exec"
)

// Adapter wraps the swiftlint binary. Bin is overridable for tests.
type Adapter struct {
	Bin string // empty → "swiftlint"
}

// New returns an Adapter using the system swiftlint.
func New() *Adapter { return &Adapter{} }

// LintInput drives Lint.
type LintInput struct {
	// Dir is the working directory for the lint. SwiftLint walks the
	// directory tree itself; this is also the directory it resolves
	// relative paths from.
	Dir string
	// ConfigPath is the optional --config flag value. Empty means
	// SwiftLint uses its own discovery rules (.swiftlint.yml in cwd).
	ConfigPath string
}

// Lint runs `swiftlint lint --reporter github-actions-logging`. When a
// ConfigPath is provided it's passed via --config. Returns the merged
// stdout+stderr output, the process exit code, and a non-nil error only
// on start/wait failure.
func (a *Adapter) Lint(ctx context.Context, in LintInput) (string, int, error) {
	args := []string{"lint"}
	if in.ConfigPath != "" {
		args = append(args, "--config", in.ConfigPath)
	}

	args = append(args, "--reporter", "github-actions-logging")
	cmd := safeexec.Command(ctx, a.bin(), args...)
	cmd.Dir = in.Dir

	var buf bytes.Buffer

	cmd.Stdout = &buf
	cmd.Stderr = &buf

	runErr := cmd.Run()
	if runErr == nil {
		return buf.String(), 0, nil
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return buf.String(), exitErr.ExitCode(), nil
	}

	return buf.String(), -1, runErr
}

func (a *Adapter) bin() string {
	if a.Bin != "" {
		return a.Bin
	}

	return "swiftlint"
}
