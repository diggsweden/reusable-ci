// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package trivy wraps the system `trivy` binary. The runtime image
// bakes trivy in; local runs need it on PATH.
package trivy

import (
	"context"
	"errors"
	"io"
	"os/exec"
)

// Adapter wraps the trivy binary. Bin is overridable for tests.
type Adapter struct {
	Bin string // empty → "trivy"
}

// New returns an Adapter using the system trivy.
func New() *Adapter { return &Adapter{} }

func (a *Adapter) bin() string {
	if a.Bin != "" {
		return a.Bin
	}
	return "trivy"
}

// RunInherit invokes `trivy` with args, streaming stdout/stderr.
// Returns the process exit code and any error from starting/waiting.
func (a *Adapter) RunInherit(ctx context.Context, stdout, stderr io.Writer, args ...string) (int, error) {
	cmd := exec.CommandContext(ctx, a.bin(), args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
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
