// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/app/toolchain"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

var errTransientMise = errors.New("transient mise failure")

type recordingMiseRunner struct {
	responses map[string]string
	fails     map[string]int
	calls     []recordingMiseCall
	lastCtx   context.Context //nolint:containedctx // recorded so cancellation reaching the runner can be asserted.
}

type recordingMiseCall struct {
	dir  string
	env  []string
	args []string
}

func (r *recordingMiseRunner) Run(ctx context.Context, env []string, args ...string) (string, error) {
	r.lastCtx = ctx

	dir := ""
	if len(args) >= 2 && args[0] == "--cd" {
		dir = args[1]
		args = args[2:]
	}

	r.calls = append(r.calls, recordingMiseCall{dir: dir, env: append([]string{}, env...), args: append([]string{}, args...)})

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
go = "1.26.6"
rust = "1.90.0"
"aqua:rust-lang/rustup" = "1.28.2"
"cargo:cargo-audit" = "0.21.2"
"aqua:foo/bar" = "1.2.3"
`)
	writeToolchainFile(t, root, "mise.lock", "# owned lock fixture\n")
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
	if !slices.EqualFunc(gotArgs, wantArgs, slices.Equal) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}

	finalEnv := finalCallEnv(t, runner)
	for _, forbidden := range []string{"GITHUB_TOKEN=must-not-leak", "MISE_FORGEJO_TOKEN=", "PIP_INDEX_URL=   "} {
		if slices.Contains(finalEnv, forbidden) {
			t.Fatalf("final env leaked %s: %#v", forbidden, finalEnv)
		}
	}

	if !slices.Contains(finalEnv, "MISE_GITHUB_TOKEN=token") || !slices.Contains(finalEnv, "MISE_LOCKED_VERIFY_PROVENANCE=0") {
		t.Fatalf("final env missing expected values: %#v", finalEnv)
	}

	if slices.Contains(finalEnv, "MISE_PARANOID=0") {
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
	writeToolchainFile(t, root, "mise.lock", "# owned lock fixture\n")

	runner := &recordingMiseRunner{}
	if err := toolchain.InstallMiseTools(context.Background(), runner, nil, toolchain.InstallMiseToolsInput{Root: root, Locked: "true", RetryAttempts: 1, RetryDelay: -1}); err != nil {
		t.Fatalf("InstallMiseTools: %v", err)
	}

	finalEnv := finalCallEnv(t, runner)
	if !slices.Contains(finalEnv, "MISE_PARANOID=0") {
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

	runner := &recordingMiseRunner{}

	err := toolchain.InstallMiseTools(context.Background(), runner, nil, toolchain.InstallMiseToolsInput{Root: t.TempDir(), Locked: "yes"})
	// A flag that is neither "true" nor "false" is a broken invocation, not a
	// toolchain problem: ErrUsage, exit 2.
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	if !strings.Contains(err.Error(), "mise-locked") {
		t.Errorf("err = %v, want it to name the flag", err)
	}

	if len(runner.calls) != 0 {
		t.Errorf("mise ran under an unparsed locked mode: %#v", runner.calls)
	}
}

// finalCallEnv returns the environment of the last mise invocation, failing
// loudly when there was none -- indexing an empty slice would panic and hide
// which assertion actually went wrong.
func finalCallEnv(t *testing.T, runner *recordingMiseRunner) []string {
	t.Helper()

	if len(runner.calls) == 0 {
		t.Fatal("mise was never invoked")
	}

	return runner.calls[len(runner.calls)-1].env
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

// TestInstallMiseTools_ExhaustedRetriesFail is the other end of the retry.
//
// TestInstallMiseTools_RetriesFinalInstallOnly fails once and recovers, so it
// cannot distinguish "retries and eventually succeeds" from "retries and
// reports success regardless". This is toolchain bootstrap: a green step that
// installed nothing leaves every later step running against whatever the
// runner image happened to ship.
func TestInstallMiseTools_ExhaustedRetriesFail(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", "/usr/bin")

	runner := &recordingMiseRunner{fails: map[string]int{"install": 99}}

	var out strings.Builder

	err := toolchain.InstallMiseTools(context.Background(), runner, &out, toolchain.InstallMiseToolsInput{
		Root: t.TempDir(), Locked: "false", RetryAttempts: 3, RetryDelay: -1,
	})
	if !errors.Is(err, errTransientMise) {
		t.Fatalf("err = %v, want the runner's own cause after the last attempt", err)
	}

	// Exactly the attempts asked for: one more would be an off-by-one in a
	// loop that shells out, one fewer means an attempt was silently dropped.
	if got := len(runner.calls); got != 3 {
		t.Errorf("mise ran %d time(s), want 3 attempts", got)
	}
}

// TestInstallMiseTools_CancellationStopsTheRetryLoop pins that a cancelled job
// stops instead of working through its remaining attempts.
//
// Each attempt shells out to mise, which downloads toolchains, so a retry loop
// that ignores cancellation keeps a cancelled CI job running and holding a
// runner for as long as its attempts last.
func TestInstallMiseTools_CancellationStopsTheRetryLoop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", "/usr/bin")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runner := &recordingMiseRunner{fails: map[string]int{"install": 99}}

	err := toolchain.InstallMiseTools(ctx, runner, nil, toolchain.InstallMiseToolsInput{
		Root: t.TempDir(), Locked: "false", RetryAttempts: 5, RetryDelay: -1,
	})
	if err == nil {
		t.Fatal("a cancelled install reported success")
	}

	if got := len(runner.calls); got > 1 {
		t.Errorf("mise ran %d time(s) after cancellation; the loop should stop", got)
	}
}

// TestInstallMiseTools_ARuntimeFailureStopsBeforeTheToolInstall pins the
// order. The runtime bootstraps run first because the requested tools are
// built against them, so continuing past a failed one installs tools onto a
// toolchain that is not there, and the error that surfaces then names the tool
// rather than the runtime that actually failed.
func TestInstallMiseTools_ARuntimeFailureStopsBeforeTheToolInstall(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", "/usr/bin")

	root := t.TempDir()
	config := "[tools]\ngo = \"1.26.6\"\n"

	if err := os.WriteFile(filepath.Join(root, "mise.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	runner := &recordingMiseRunner{fails: map[string]int{"install\x00go": 99}}

	err := toolchain.InstallMiseTools(context.Background(), runner, nil, toolchain.InstallMiseToolsInput{
		Root: root, Locked: "false", Tools: "go", RetryAttempts: 2, RetryDelay: -1,
	})
	if !errors.Is(err, errTransientMise) {
		t.Fatalf("err = %v, want the runtime failure", err)
	}

	// The runtime bootstrap is not retried, only the final install is, so
	// exactly one call and nothing after it.
	if got := len(runner.calls); got != 1 {
		t.Errorf("mise ran %d time(s); a failed runtime must stop the sequence: %+v", got, runner.calls)
	}
}

// TestInstallMiseTools_RefusesSelectorsOutsideTheDeclaredSet: the requested
// tools must name [tools] keys exactly. An undeclared tool, a flag, a version
// or backend selector for a declared tool, and a repeated name are all usage
// errors before mise is run at all, so nothing outside the reviewed
// configuration is resolved.
func TestInstallMiseTools_RefusesSelectorsOutsideTheDeclaredSet(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	root := t.TempDir()
	writeToolchainFile(t, root, ".mise.toml", "[tools]\ngo = \"1.26.6\"\n\"aqua:foo/bar\" = \"1.2.3\"\n")

	for _, tools := range []string{
		"node",
		"--yes",
		"go -y",
		"go@1.22",
		"aqua:foo/bar@latest",
		"aqua:foo/other",
		"go go",
		"GO",
	} {
		runner := &recordingMiseRunner{}

		err := toolchain.InstallMiseTools(context.Background(), runner, nil, toolchain.InstallMiseToolsInput{Root: root, Locked: "false", Tools: tools, RetryAttempts: 1, RetryDelay: -1})
		if !errors.Is(err, errs.ErrUsage) || err == nil || !strings.Contains(err.Error(), "unique subset of [tools]") {
			t.Errorf("tools %q: err = %v, want the subset refusal", tools, err)
		}

		if len(runner.calls) != 0 {
			t.Errorf("tools %q: mise ran %d time(s)", tools, len(runner.calls))
		}
	}

	runner := &recordingMiseRunner{}
	if err := toolchain.InstallMiseTools(context.Background(), runner, nil, toolchain.InstallMiseToolsInput{Root: root, Locked: "false", Tools: "aqua:foo/bar  go", RetryAttempts: 1, RetryDelay: -1}); err != nil {
		t.Fatal(err)
	}

	final := runner.calls[len(runner.calls)-1].args
	if want := []string{"install", "aqua:foo/bar", "go"}; !slices.Equal(final, want) {
		t.Errorf("final install = %q, want the requested subset in the order given %q", final, want)
	}
}

// TestMiseUseCases_NilRunnerIsAUsageError: exposing and installing mise tools
// without a runner is a wiring mistake refused before any file is read or
// written. The system-dependency, trust and changelog-renderer refusals are
// pinned beside their own tests.
func TestMiseUseCases_NilRunnerIsAUsageError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeToolchainFile(t, root, ".mise.toml", "[tools]\ngo = \"1.26.6\"\n")
	pathFile := filepath.Join(t.TempDir(), "path")

	if err := toolchain.InstallMiseTools(context.Background(), nil, nil, toolchain.InstallMiseToolsInput{Root: root, Locked: "false"}); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("install: err = %v", err)
	}

	if err := toolchain.ExposeMiseTools(context.Background(), nil, nil, toolchain.ExposeMiseToolsInput{Root: root, BinHome: t.TempDir(), PathFile: pathFile, Locked: "false"}); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("expose: err = %v", err)
	}

	if _, err := os.Stat(pathFile); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("refusal wrote the path file: %v", err)
	}
}
