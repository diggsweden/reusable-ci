// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package npm shells out to the system `npm` binary. Used by the
// version-bump use case to run `npm version <v> --no-git-tag-version`.
package npm

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Adapter wraps the npm binary. Bin is overridable for tests.
type Adapter struct {
	Bin string // empty → "npm"
}

// New returns an Adapter using the system npm.
func New() *Adapter { return &Adapter{} }

func (a *Adapter) bin() string {
	if a.Bin != "" {
		return a.Bin
	}
	return "npm"
}

// RunInherit invokes `npm` with args and streams stdout/stderr to the
// provided writers. The bash uses npm directly; this matches the shape.
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
