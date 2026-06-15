// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package opengrep wraps the system `opengrep` binary. The runtime
// image bakes opengrep in; local runs need it on PATH.
package opengrep

import (
	"context"
	"errors"
	"github.com/diggsweden/reusable-ci/internal/safeexec"
	"io"
	"os/exec"
)

// Adapter wraps the opengrep binary. Bin is overridable for tests.
type Adapter struct {
	Bin string // empty → "opengrep"
}

// New returns an Adapter using the system opengrep.
func New() *Adapter { return &Adapter{} }

// RunInherit invokes `opengrep` with args, streaming stdout/stderr to
// the provided writers. Returns the process exit code (0 on success)
// and any error from starting/waiting on the process.
//
// The bash sets PYTHONWARNINGS to suppress a noisy
// RequestsDependencyWarning before invoking opengrep; we do the same
// via the child's environment.
func (a *Adapter) RunInherit(ctx context.Context, stdout, stderr io.Writer, args ...string) (int, error) {
	cmd := safeexec.Command(ctx, a.bin(), args...)
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

	// A non-exit failure (most commonly opengrep missing from PATH) is an
	// external-dependency problem, not an internal bug — classify it as
	// EX_UNAVAILABLE (69) rather than the unclassified EX_SOFTWARE (70).
	return -1, safeexec.WrapError(err, a.bin(), safeexec.FirstArg(args))
}

func (a *Adapter) bin() string {
	if a.Bin != "" {
		return a.Bin
	}

	return "opengrep"
}
