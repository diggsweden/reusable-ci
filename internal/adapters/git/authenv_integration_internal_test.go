//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// TestAuthEnv_GitAppliesTheHeaderOnlyForTheRemoteAndNeverStoresIt is the
// positive control the persistence test lacks. That test fetches from a local
// path under a header scoped to an https URL, so the header is never used and
// a broken environment would pass. Here git itself is asked which extraheader
// applies: the scoped key resolves for the remote's URL and not for another,
// and the repository's own config file is unchanged afterwards.
func TestAuthEnv_GitAppliesTheHeaderOnlyForTheRemoteAndNeverStoresIt(t *testing.T) {
	t.Parallel()

	const remote = "https://forge.example.invalid/owner/repo.git"

	dir := t.TempDir()
	home := t.TempDir()

	git := func(t *testing.T, extra []string, args ...string) (string, error) {
		t.Helper()

		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // fixed git subcommands against an owned temp repository.
		cmd.Dir = dir
		cmd.Env = append([]string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}, extra...)
		out, err := cmd.Output()

		return strings.TrimSpace(string(out)), err
	}

	if _, err := git(t, nil, "init", "-q"); err != nil {
		t.Fatalf("git init: %v", err)
	}

	before, err := os.ReadFile(filepath.Join(dir, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}

	env := authEnv(remote, runcontext.OperatorCredential("fixture-token"))

	got, err := git(t, env, "config", "--get-urlmatch", "http.extraheader", remote)
	if err != nil || got != "Authorization: Basic eC1hY2Nlc3MtdG9rZW46Zml4dHVyZS10b2tlbg==" {
		t.Errorf("header for the remote = %q (err %v), want the Basic header", got, err)
	}

	if other, otherErr := git(t, env, "config", "--get-urlmatch", "http.extraheader", "https://elsewhere.example.invalid/owner/repo.git"); otherErr == nil || other != "" {
		t.Errorf("header for another host = %q (err %v), want none", other, otherErr)
	}

	after, err := os.ReadFile(filepath.Join(dir, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}

	if string(after) != string(before) || strings.Contains(string(after), "fixture-token") {
		t.Errorf(".git/config changed:\n%s", after)
	}
}
