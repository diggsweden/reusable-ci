// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package git is the thin shell-out adapter around the `git` CLI.
//
// Each method is one git invocation, returning typed values + sentinel
// errors. Callers in app/ orchestrate sequences of these. Domain code
// never touches this package — git operations are I/O.
package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Repo is a handle for git operations against a working tree.
// The Dir field, when non-empty, sets the cwd for every invocation.
type Repo struct {
	Dir    string
	GitBin string // override `git` binary path; empty → exec.LookPath("git")
}

// New returns a Repo that runs git against the current working directory
// (Dir == ""). For test isolation, set Dir to t.TempDir().
func New() *Repo { return &Repo{} }

// Run executes `git <args...>` and returns its trimmed stdout. Combined
// stderr is included in the returned error on non-zero exit.
func (r *Repo) Run(ctx context.Context, args ...string) (string, error) {
	bin := r.GitBin
	if bin == "" {
		bin = "git"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	if r.Dir != "" {
		cmd.Dir = r.Dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// RunStdin is Run with a stdin string (used for things like signing input
// or piping commands to git-connect-style tools).
func (r *Repo) RunStdin(ctx context.Context, stdin string, args ...string) (string, error) {
	bin := r.GitBin
	if bin == "" {
		bin = "git"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	if r.Dir != "" {
		cmd.Dir = r.Dir
	}
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimRight(string(out), "\n"), nil
}
