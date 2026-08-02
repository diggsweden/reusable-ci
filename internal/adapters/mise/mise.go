// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package mise shells out to the mise CLI for toolchain introspection.
package mise

import (
	"context"
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
)

// Runner invokes mise. Bin is a test seam; empty uses "mise".
type Runner struct {
	Bin string
}

// New returns a default mise runner.
func New() *Runner { return &Runner{} }

// Run executes mise with typed args and optional environment overrides.
func (r *Runner) Run(ctx context.Context, env []string, args ...string) (string, error) {
	bin := r.Bin
	if bin == "" {
		bin = "mise"
	}

	cmd := safeexec.Command(ctx, bin, args...)
	if env != nil {
		cmd.Env = env
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		wrapped := safeexec.WrapError(err, bin, safeexec.FirstArg(args))
		if len(out) == 0 {
			return "", wrapped
		}

		return "", fmt.Errorf("%w\n%s", wrapped, safeexec.RedactKeyMaterial(out))
	}

	return strings.TrimRight(string(out), "\n"), nil
}
