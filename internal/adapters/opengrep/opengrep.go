// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package opengrep wraps the system `opengrep` binary. The runtime
// image bakes opengrep in; local runs need it on PATH.
package opengrep

import (
	"context"
	"errors"
	"io"
	"os/exec"
)

// Adapter wraps the opengrep binary. Bin is overridable for tests.
type Adapter struct {
	Bin string // empty → "opengrep"
}

// New returns an Adapter using the system opengrep.
func New() *Adapter { return &Adapter{} }

func (a *Adapter) bin() string {
	if a.Bin != "" {
		return a.Bin
	}
	return "opengrep"
}

// RunInherit invokes `opengrep` with args, streaming stdout/stderr to
// the provided writers. Returns the process exit code (0 on success)
// and any error from starting/waiting on the process.
//
// The bash sets PYTHONWARNINGS to suppress a noisy
// RequestsDependencyWarning before invoking opengrep; we do the same
// via the child's environment.
func (a *Adapter) RunInherit(ctx context.Context, stdout, stderr io.Writer, args ...string) (int, error) {
	cmd := exec.CommandContext(ctx, a.bin(), args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// Append rather than replace — preserve whatever the caller had set.
	cmd.Env = append(cmd.Environ(), "PYTHONWARNINGS=ignore:RequestsDependencyWarning")
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return -1, err
}
