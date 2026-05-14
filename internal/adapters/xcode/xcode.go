// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package xcode wraps the macOS toolchain (`xcodebuild`, `security`,
// `xcbeautify`). All adapters here only run on darwin runners; the
// linux unit tests use Bin override + mockbinary.
package xcode

import (
	"context"
	"errors"
	"io"
	"os/exec"
)

// XcodeBuild wraps the `xcodebuild` binary.
type XcodeBuild struct {
	Bin string // empty → "xcodebuild"
}

// NewXcodeBuild returns an XcodeBuild using the system binary.
func NewXcodeBuild() *XcodeBuild { return &XcodeBuild{} }

func (a *XcodeBuild) bin() string {
	if a.Bin != "" {
		return a.Bin
	}
	return "xcodebuild"
}

// RunInherit invokes xcodebuild with args, streaming stdout/stderr.
// Returns the exit code and an error if the process couldn't start.
func (a *XcodeBuild) RunInherit(ctx context.Context, stdout, stderr io.Writer, args ...string) (int, error) {
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

// Security wraps the macOS `security` keychain CLI.
type Security struct {
	Bin string // empty → "security"
}

// NewSecurity returns a Security using the system binary.
func NewSecurity() *Security { return &Security{} }

func (a *Security) bin() string {
	if a.Bin != "" {
		return a.Bin
	}
	return "security"
}

// Run invokes `security <args>`. Returns captured combined output.
func (a *Security) Run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, a.bin(), args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}
