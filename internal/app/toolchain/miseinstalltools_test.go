// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/app/toolchain"
)

var errTransientMise = errors.New("transient mise failure")

type recordingMiseRunner struct {
	responses map[string]string
	fails     map[string]int
	calls     []recordingMiseCall
}

type recordingMiseCall struct {
	env  []string
	args []string
}

func (r *recordingMiseRunner) Run(_ context.Context, env []string, args ...string) (string, error) {
	r.calls = append(r.calls, recordingMiseCall{env: append([]string{}, env...), args: append([]string{}, args...)})

	key := strings.Join(args, "\x00")
	if r.fails != nil && r.fails[key] > 0 {
		r.fails[key]--

		return "", errTransientMise
	}

	if value, ok := r.responses[key]; ok {
		return value, nil
	}

	return "", nil
}

func TestInstallMiseTools_BootstrapsRuntimesRustupAndSubset(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("MISE_GITHUB_TOKEN", "token")
	t.Setenv("MISE_FORGEJO_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "must-not-leak")
	t.Setenv("PIP_INDEX_URL", "   ")

	root := t.TempDir()
	writeToolchainFile(t, root, ".mise.toml", `[tools]
uv = "0.9.0"
go = "1.26"
rust = "1.90"
"aqua:rust-lang/rustup" = "1.28"
"cargo:cargo-audit" = "0.21"
`)
	writeToolchainFile(t, root, "rust-toolchain.toml", "[toolchain]\nchannel = \"1.90.0\"\n")
	cargoBin := filepath.Join(root, "cargo", "bin")
	runner := &recordingMiseRunner{responses: map[string]string{
		"exec\x00--no-deps\x00--locked\x00aqua:rust-lang/rustup\x00--\x00rustup\x00which\x00cargo": filepath.Join(cargoBin, "cargo"),
	}}

	err := toolchain.InstallMiseTools(context.Background(), runner, nil, toolchain.InstallMiseToolsInput{
		Root:          root,
		Locked:        "true",
		Tools:         "cargo:cargo-audit aqua:foo/bar",
		RetryAttempts: 1,
		RetryDelay:    -1,
	})
	if err != nil {
		t.Fatalf("InstallMiseTools: %v", err)
	}

	gotArgs := make([][]string, 0, len(runner.calls))
	for _, call := range runner.calls {
		gotArgs = append(gotArgs, call.args)
	}

	wantArgs := [][]string{
		{"install", "--locked", "uv"},
		{"install", "--locked", "go"},
		{"install", "--locked", "rust"},
		{"install", "--locked", "aqua:rust-lang/rustup"},
		{"exec", "--no-deps", "--locked", "aqua:rust-lang/rustup", "--", "rustup", "show"},
		{"exec", "--no-deps", "--locked", "aqua:rust-lang/rustup", "--", "rustup", "which", "cargo"},
		{"install", "--locked", "cargo:cargo-audit", "aqua:foo/bar"},
	}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %#v", gotArgs)
	}

	finalEnv := runner.calls[len(runner.calls)-1].env
	for _, forbidden := range []string{"GITHUB_TOKEN=must-not-leak", "MISE_FORGEJO_TOKEN=", "PIP_INDEX_URL=   "} {
		if envContains(finalEnv, forbidden) {
			t.Fatalf("final env leaked %s: %#v", forbidden, finalEnv)
		}
	}

	if !envContains(finalEnv, "MISE_GITHUB_TOKEN=token") || !envContains(finalEnv, "MISE_LOCKED_VERIFY_PROVENANCE=0") {
		t.Fatalf("final env missing expected values: %#v", finalEnv)
	}

	if envContains(finalEnv, "MISE_PARANOID=0") {
		t.Fatalf("MISE_PARANOID should not be forced when GitHub token is present: %#v", finalEnv)
	}

	pathValue := envValue(finalEnv, "PATH")
	if !strings.HasPrefix(pathValue, cargoBin+string(os.PathListSeparator)) {
		t.Fatalf("PATH = %q, want cargo dir prepended", pathValue)
	}
}

func TestInstallMiseTools_LockedWithoutGitHubTokenForcesParanoid(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("MISE_GITHUB_TOKEN", "")

	root := t.TempDir()

	runner := &recordingMiseRunner{}
	if err := toolchain.InstallMiseTools(context.Background(), runner, nil, toolchain.InstallMiseToolsInput{Root: root, Locked: "true", RetryAttempts: 1, RetryDelay: -1}); err != nil {
		t.Fatalf("InstallMiseTools: %v", err)
	}

	finalEnv := runner.calls[len(runner.calls)-1].env
	if !envContains(finalEnv, "MISE_PARANOID=0") {
		t.Fatalf("final env missing MISE_PARANOID=0: %#v", finalEnv)
	}
}

func TestInstallMiseTools_RetriesFinalInstallOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", "/usr/bin")

	runner := &recordingMiseRunner{fails: map[string]int{"install": 1}}

	var out strings.Builder
	if err := toolchain.InstallMiseTools(context.Background(), runner, &out, toolchain.InstallMiseToolsInput{Root: t.TempDir(), Locked: "false", RetryAttempts: 2, RetryDelay: -1}); err != nil {
		t.Fatalf("InstallMiseTools: %v", err)
	}

	if got := len(runner.calls); got != 2 {
		t.Fatalf("calls = %d, want final install retry", got)
	}

	if !strings.Contains(out.String(), "retrying") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestInstallMiseTools_RejectsBadLockedMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	err := toolchain.InstallMiseTools(context.Background(), &recordingMiseRunner{}, nil, toolchain.InstallMiseToolsInput{Root: t.TempDir(), Locked: "yes"})
	if err == nil || !strings.Contains(fmt.Sprint(err), "mise-locked") {
		t.Fatalf("err = %v, want mise-locked validation", err)
	}
}

func envValue(env []string, name string) string {
	for _, item := range env {
		got, value, ok := strings.Cut(item, "=")
		if ok && got == name {
			return value
		}
	}

	return ""
}
