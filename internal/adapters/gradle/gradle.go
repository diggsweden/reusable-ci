// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package gradle shells out to the project-local `./gradlew` wrapper.
// Pure helpers (init-script rendering, summary rendering) live in
// internal/domain/build.
package gradle

import (
	"context"
	"io"
	"os/exec"

	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
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
	return a.RunInDirEnvInherit(ctx, dir, nil, stdout, stderr, args...)
}

// RunInDirEnvInherit runs the wrapper with explicit environment overrides.
// Unselected runtime variables remain inherited, including JDK/SDK settings.
func (a *Adapter) RunInDirEnvInherit(ctx context.Context, dir string, env []string, stdout, stderr io.Writer, args ...string) error {
	cmd := a.command(ctx, dir, env, stdout, stderr, args...)
	if err := cmd.Run(); err != nil {
		return safeexec.WrapError(err, a.bin(), safeexec.FirstArg(args))
	}

	return nil
}

func (a *Adapter) command(ctx context.Context, dir string, env []string, stdout, stderr io.Writer, args ...string) *exec.Cmd {
	cmd := safeexec.Command(ctx, a.bin(), args...)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = append(cmd.Environ(), env...)
	// exec applies last-value-wins; normalize now so the command itself exposes
	// the exact effective environment to pure builder tests.
	cmd.Env = cmd.Environ()

	return cmd
}

func (a *Adapter) bin() string {
	if a.Bin != "" {
		return a.Bin
	}

	return "./gradlew"
}
