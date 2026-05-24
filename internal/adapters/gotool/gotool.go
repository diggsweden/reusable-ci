// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package gotool shells out to Go ecosystem binaries.
package gotool

import (
	"context"
	"os"

	domainbuild "github.com/diggsweden/reusable-ci/internal/domain/build"
	"github.com/diggsweden/reusable-ci/internal/safeexec"
)

// Go invokes the local go binary.
type Go struct{}

// Run executes go with inherited environment plus in.Env. Errors are
// classified by safeexec.WrapError so missing-on-PATH maps to
// ExitCodeUnavailable and a non-zero exit maps to ExitCodeValidation.
func (Go) Run(ctx context.Context, in domainbuild.GoRunInput) error {
	return run(ctx, "go", in)
}

// CycloneDXGoMod invokes the local cyclonedx-gomod binary.
type CycloneDXGoMod struct{}

// Run executes cyclonedx-gomod with inherited environment plus in.Env.
func (CycloneDXGoMod) Run(ctx context.Context, in domainbuild.GoRunInput) error {
	return run(ctx, "cyclonedx-gomod", in)
}

// run is the shared subprocess driver. Centralising it here keeps the
// error-wrap policy (delegated to safeexec.WrapError) in one place.
func run(ctx context.Context, bin string, in domainbuild.GoRunInput) error {
	cmd := safeexec.Command(ctx, bin, in.Args...)
	cmd.Dir = in.Dir
	cmd.Env = append(os.Environ(), in.Env...)
	cmd.Stdout = in.Stdout
	cmd.Stderr = in.Stderr

	return safeexec.WrapError(cmd.Run(), bin, subcommand(in.Args))
}

// subcommand returns the leading positional argument (e.g. "test",
// "build", "mod") for human-readable error context.
func subcommand(args []string) string {
	if len(args) == 0 {
		return ""
	}

	return args[0]
}
