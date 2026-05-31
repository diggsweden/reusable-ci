// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package xcode wraps the macOS toolchain (`xcodebuild`, `security`,
// `xcbeautify`). All adapters here only run on darwin runners; the
// linux unit tests use Bin override + mockbinary.
package xcode

import (
	"context"
	"errors"
	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
	"io"
	"os/exec"
)

// Build wraps the `xcodebuild` binary.
type Build struct {
	Bin string // empty → "xcodebuild"
}

// NewBuild returns an Build using the system binary.
func NewBuild() *Build { return &Build{} }

// RunInherit invokes xcodebuild with args, streaming stdout/stderr.
// Returns the exit code and an error if the process couldn't start.
func (a *Build) RunInherit(ctx context.Context, stdout, stderr io.Writer, args ...string) (int, error) {
	cmd := safeexec.Command(ctx, a.bin(), args...)
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

	// A non-exit failure (most commonly xcodebuild missing from PATH) is an
	// external-dependency problem, not an internal bug — classify it as
	// EX_UNAVAILABLE (69) rather than the unclassified EX_SOFTWARE (70).
	return -1, safeexec.WrapError(err, a.bin(), safeexec.FirstArg(args))
}

func (a *Build) bin() string {
	if a.Bin != "" {
		return a.Bin
	}

	return "xcodebuild"
}

// Security wraps the macOS `security` keychain CLI.
type Security struct {
	Bin string // empty → "security"
}

// NewSecurity returns a Security using the system binary.
func NewSecurity() *Security { return &Security{} }

// Run invokes `security <args>`. Returns captured combined output,
// scrubbed through safeexec.RedactKeyMaterial so a keychain operation
// that echoes PEM-encoded certificate or key material (`security
// find-identity`, `security export -k`, etc.) doesn't propagate that
// material into caller error messages or step summaries. The `security`
// tool is the highest-likelihood source of PEM bytes in this codebase.
func (a *Security) Run(ctx context.Context, args ...string) (string, error) {
	cmd := safeexec.Command(ctx, a.bin(), args...)

	out, err := cmd.CombinedOutput()

	return string(safeexec.RedactKeyMaterial(out)), err
}

func (a *Security) bin() string {
	if a.Bin != "" {
		return a.Bin
	}

	return "security"
}
