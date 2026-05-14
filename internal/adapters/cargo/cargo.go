// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package cargo shells out to the system `cargo` binary. Used by the
// version-bump use case to refresh Cargo.lock after rewriting
// Cargo.toml's version line.
package cargo

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Adapter wraps the cargo binary. Bin is overridable for tests.
type Adapter struct {
	Bin string // empty → "cargo"
}

// New returns an Adapter using the system cargo.
func New() *Adapter { return &Adapter{} }

func (a *Adapter) bin() string {
	if a.Bin != "" {
		return a.Bin
	}
	return "cargo"
}

// Available reports whether the cargo binary is on PATH. Mirrors the
// bash `command -v cargo` check.
func (a *Adapter) Available() bool {
	_, err := exec.LookPath(a.bin())
	return err == nil
}

// RunInherit invokes cargo with args inside dir, streaming output.
func (a *Adapter) RunInherit(ctx context.Context, dir string, stdout, stderr io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, a.bin(), args...)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", a.bin(), strings.Join(args, " "), err)
	}
	return nil
}
