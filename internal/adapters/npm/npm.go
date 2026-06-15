// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package npm shells out to the system `npm` binary. Used by the
// version-bump use case to run `npm version <v> --no-git-tag-version`.
package npm

import (
	"bytes"
	"context"
	"github.com/diggsweden/reusable-ci/internal/safeexec"
	"io"
)

// Adapter wraps the npm binary. Bin is overridable for tests.
type Adapter struct {
	Bin string // empty → "npm"
}

// Run captures stdout/stderr from `npm` while still returning the command
// failure so callers can distinguish expected npm failures from parse errors.
func (a *Adapter) Run(ctx context.Context, dir string, args ...string) (string, string, error) {
	cmd := safeexec.Command(ctx, a.bin(), args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return stdout.String(), stderr.String(), safeexec.WrapError(err, a.bin(), safeexec.FirstArg(args))
	}

	return stdout.String(), stderr.String(), nil
}

// New returns an Adapter using the system npm.
func New() *Adapter { return &Adapter{} }

// RunInherit invokes `npm` with args and streams stdout/stderr to the
// provided writers. The bash uses npm directly; this matches the shape.
func (a *Adapter) RunInherit(ctx context.Context, dir string, stdout, stderr io.Writer, args ...string) error {
	cmd := safeexec.Command(ctx, a.bin(), args...)
	cmd.Dir = dir
	cmd.Stdout = stdout

	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return safeexec.WrapError(err, a.bin(), safeexec.FirstArg(args))
	}

	return nil
}

func (a *Adapter) bin() string {
	if a.Bin != "" {
		return a.Bin
	}

	return "npm"
}
