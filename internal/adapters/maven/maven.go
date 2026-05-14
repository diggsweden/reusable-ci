// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package maven shells out to the real `mvn` binary. Pure helpers
// (snapshot detection, summary rendering) live in
// internal/domain/build.
package maven

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Adapter wraps the mvn binary. MvnBin is overridable for tests.
type Adapter struct {
	MvnBin string // empty → "mvn"
}

// New returns an Adapter using the system mvn.
func New() *Adapter { return &Adapter{} }

func (a *Adapter) bin() string {
	if a.MvnBin != "" {
		return a.MvnBin
	}
	return "mvn"
}

// EvalExpression returns the value of a Maven expression as printed by
// `mvn help:evaluate -Dexpression=<expr> -q -DforceStdout`. Used to read
// project.{version,groupId,artifactId} without parsing the POM.
//
// Stdout is returned trimmed of a single trailing newline; combined
// output is included in the error on failure.
func (a *Adapter) EvalExpression(ctx context.Context, expr string) (string, error) {
	args := []string{"help:evaluate", "-Dexpression=" + expr, "-q", "-DforceStdout"}
	cmd := exec.CommandContext(ctx, a.bin(), args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w\n%s", a.bin(), strings.Join(args, " "), err, out)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// RunInherit invokes `mvn` with the given arguments and streams stdout
// and stderr to the provided writers. Build/test/package steps want this
// path so the user sees Maven's own progress in the CI log.
func (a *Adapter) RunInherit(ctx context.Context, stdout, stderr io.Writer, args ...string) error {
	return a.RunInheritIn(ctx, "", stdout, stderr, args...)
}

// RunInheritIn is like RunInherit but runs mvn inside dir. dir="" runs
// in the current working directory.
func (a *Adapter) RunInheritIn(ctx context.Context, dir string, stdout, stderr io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, a.bin(), args...)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", a.bin(), strings.Join(args, " "), err)
	}
	return nil
}
