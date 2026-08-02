// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package apt wraps apt-get for Debian-family CI runner bootstrap tasks.
package apt

import (
	"context"
	"io"
	"os"

	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
)

// Adapter runs apt-get.
type Adapter struct {
	Bin string
}

// New returns an Adapter using apt-get from PATH.
func New() *Adapter { return &Adapter{} }

// Install refreshes package indexes and installs packages without recommends.
func (a *Adapter) Install(ctx context.Context, packages []string, out io.Writer) error {
	env := append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")

	if err := a.run(ctx, out, env, "update"); err != nil {
		return err
	}

	args := append([]string{"install", "-y", "--no-install-recommends"}, packages...)

	return a.run(ctx, out, env, args...)
}

func (a *Adapter) run(ctx context.Context, out io.Writer, env []string, args ...string) error {
	cmd := safeexec.Command(ctx, a.bin(), args...)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		return safeexec.WrapError(err, a.bin(), safeexec.FirstArg(args))
	}

	return nil
}

func (a *Adapter) bin() string {
	if a.Bin != "" {
		return a.Bin
	}

	return "apt-get"
}
