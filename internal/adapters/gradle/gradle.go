// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package gradle shells out to the project-local `./gradlew` wrapper.
// Pure helpers (init-script rendering, summary rendering) live in
// internal/domain/build.
package gradle

import (
	"context"
	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
	"io"
)

// Adapter wraps the gradle wrapper. Bin is overridable for tests; the
// production path is the project-local "./gradlew".
type Adapter struct {
	Bin string // empty → "./gradlew"
}

// New returns an Adapter using the project-local ./gradlew.
func New() *Adapter { return &Adapter{} }

// RunInherit invokes ./gradlew with the given arguments and streams
// stdout and stderr to the provided writers. The build / SBOM phases
// want this so the user sees gradle's own progress in CI.
func (a *Adapter) RunInherit(ctx context.Context, stdout, stderr io.Writer, args ...string) error {
	return a.RunInDirInherit(ctx, "", stdout, stderr, args...)
}

// RunInDirInherit invokes ./gradlew in dir and streams stdout/stderr.
func (a *Adapter) RunInDirInherit(ctx context.Context, dir string, stdout, stderr io.Writer, args ...string) error {
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

	return "./gradlew"
}
