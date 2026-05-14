// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package gradle shells out to the project-local `./gradlew` wrapper.
// Pure helpers (init-script rendering, summary rendering) live in
// internal/domain/build.
package gradle

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Adapter wraps the gradle wrapper. Bin is overridable for tests; the
// production path is the project-local "./gradlew".
type Adapter struct {
	Bin string // empty → "./gradlew"
}

// New returns an Adapter using the project-local ./gradlew.
func New() *Adapter { return &Adapter{} }

func (a *Adapter) bin() string {
	if a.Bin != "" {
		return a.Bin
	}
	return "./gradlew"
}

// RunInherit invokes ./gradlew with the given arguments and streams
// stdout and stderr to the provided writers. The build / SBOM phases
// want this so the user sees gradle's own progress in CI.
func (a *Adapter) RunInherit(ctx context.Context, stdout, stderr io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, a.bin(), args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", a.bin(), strings.Join(args, " "), err)
	}
	return nil
}
