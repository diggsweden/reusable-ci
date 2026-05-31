// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package cargo shells out to the system `cargo` binary. Used by the
// version-bump use case to refresh Cargo.lock after rewriting
// Cargo.toml's version line.
package cargo

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"

	domainbuild "github.com/diggsweden/reusable-ci/v3/internal/domain/build"
	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
)

// Adapter wraps the cargo binary. Bin is overridable for tests.
type Adapter struct {
	Bin string // empty → "cargo"
}

// New returns an Adapter using the system cargo.
func New() *Adapter { return &Adapter{} }

// Available reports whether the cargo binary is on PATH. Mirrors the
// bash `command -v cargo` check.
func (a *Adapter) Available() bool {
	_, err := exec.LookPath(a.bin())

	return err == nil
}

// Version returns `cargo --version` output.
func (a *Adapter) Version(ctx context.Context) (string, error) {
	out, err := safeexec.Command(ctx, a.bin(), "--version").CombinedOutput()

	return strings.TrimSpace(string(out)), err
}

// RunInherit invokes cargo with args inside dir, streaming output.
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

// Run implements appbuild.CargoTool. Args/dir/stdout/stderr come from
// the input struct; env (if non-empty) is forwarded alongside os.Environ
// so cross-linker overrides (e.g. CARGO_TARGET_AARCH64_UNKNOWN_LINUX_GNU_LINKER)
// reach cargo.
func (a *Adapter) Run(ctx context.Context, in domainbuild.GoRunInput) error {
	cmd := safeexec.Command(ctx, a.bin(), in.Args...)
	cmd.Dir = in.Dir
	cmd.Stdout = in.Stdout
	cmd.Stderr = in.Stderr

	if len(in.Env) > 0 {
		cmd.Env = append(os.Environ(), in.Env...)
	}

	if err := cmd.Run(); err != nil {
		return safeexec.WrapError(err, a.bin(), safeexec.FirstArg(in.Args))
	}

	return nil
}

func (a *Adapter) bin() string {
	if a.Bin != "" {
		return a.Bin
	}

	return "cargo"
}
