// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package docker shells out to the system `docker` binary.
package docker

import (
	"bytes"
	"context"
	"github.com/diggsweden/reusable-ci/internal/safeexec"
	"io"
)

// Adapter wraps the docker binary. Bin is overridable for tests.
type Adapter struct {
	Bin string // empty -> "docker"
}

// New returns an Adapter using the system docker.
func New() *Adapter { return &Adapter{} }

// Run captures stdout/stderr from `docker`. Stderr is passed through
// safeexec.RedactKeyMaterial before being returned — a registry-auth
// failure could in principle include bearer-token bytes in the docker
// error path; the redactor catches PEM markers and JWT-shaped tokens.
// Stdout is left as-is (the typical use is to capture a digest or
// manifest JSON, neither of which is secret-shaped).
func (a *Adapter) Run(ctx context.Context, args ...string) (string, string, error) {
	cmd := safeexec.Command(ctx, a.bin(), args...)

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout

	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), string(safeexec.RedactKeyMaterial(stderr.Bytes())), safeexec.WrapError(err, a.bin(), firstArgOf(args))
	}

	return stdout.String(), string(safeexec.RedactKeyMaterial(stderr.Bytes())), nil
}

// RunInherit invokes `docker` with args and streams stdout/stderr to the
// provided writers.
func (a *Adapter) RunInherit(ctx context.Context, stdout, stderr io.Writer, args ...string) error {
	cmd := safeexec.Command(ctx, a.bin(), args...)
	cmd.Stdout = stdout

	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return safeexec.WrapError(err, a.bin(), firstArgOf(args))
	}

	return nil
}

func (a *Adapter) bin() string {
	if a.Bin != "" {
		return a.Bin
	}

	return "docker"
}

// firstArgOf returns the leading positional argument for use in
// human-readable error context. Empty when args is empty.
func firstArgOf(args []string) string {
	if len(args) == 0 {
		return ""
	}

	return args[0]
}
