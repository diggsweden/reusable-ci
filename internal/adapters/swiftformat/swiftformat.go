// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package swiftformat wraps the system `swift-format` binary.
// Installed via `brew install swift-format[@version]` in the workflow;
// the binary is only available on macOS runners.
package swiftformat

import (
	"bytes"
	"context"
	"errors"
	"github.com/diggsweden/reusable-ci/internal/safeexec"
	"os/exec"
)

// Adapter wraps the swift-format binary. Bin is overridable for tests.
type Adapter struct {
	Bin string // empty → "swift-format"
}

// New returns an Adapter using the system swift-format.
func New() *Adapter { return &Adapter{} }

// Lint runs `swift-format lint -s` against files (each path is passed
// as a positional argument). Returns the merged stdout+stderr output,
// the process exit code, and a non-nil error only on start/wait failure.
// A non-zero exit code with err == nil signals "swift-format ran but
// found issues" — the caller decides whether to fail.
func (a *Adapter) Lint(ctx context.Context, dir string, files []string) (string, int, error) {
	args := append([]string{"lint", "-s"}, files...)
	cmd := safeexec.Command(ctx, a.bin(), args...)
	cmd.Dir = dir

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

	return "swift-format"
}
