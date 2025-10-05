//go:build smoke

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// This tier is package main_test on purpose. docs/testing.md defines the smoke
// tier as driving "the binary as a black box, in this repo", and being inside
// package main would let a test here call formatPanic or watchSignals directly
// -- still compiling, still passing, no longer a black-box test. The boundary
// is what keeps the description true.
package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// smoke is the state TestMain prepares once for the whole package: the shared
// build root, the package directory the test process started in, and the
// binary built on first use.
var smoke struct {
	baseTemp   string
	packageDir string
	buildOnce  sync.Once
	binary     string
	buildErr   error
}

// smokeWorkDir is the empty working directory children start in when their
// test did not choose one.
func smokeWorkDir() string { return filepath.Join(smoke.baseTemp, "work") }

// refusingProxy is where every child's HTTP(S) proxy points: nothing listens
// on the discard port, so a command that reaches for the network fails at
// connect instead of leaving the host. Go never proxies loopback, and this
// tier starts no servers.
const refusingProxy = "http://127.0.0.1:9"

// TestMain creates the shared build root and, inside it, an empty working
// directory for children. A child run from the package directory used to start
// in cmd/reusable-ci, so a relative default output landed in the repository;
// runBinary now starts such a child in the owned directory and requires it to
// stay empty.
func TestMain(m *testing.M) {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	dir, err := os.MkdirTemp("", "reusable-ci-smoke-*")
	if err != nil {
		panic(err)
	}

	smoke.baseTemp = dir
	smoke.packageDir = wd

	if err := os.Mkdir(smokeWorkDir(), 0o700); err != nil {
		panic(err)
	}

	code := m.Run()
	os.Exit(cleanupSmokeRoot(dir, os.Stderr, code))
}

// cleanupSmokeRoot removes the shared build root and folds a failure into the
// exit code.
//
// The removal used to be `_ = os.RemoveAll(dir)`. What it removes is a freshly
// built binary per run, outside t.TempDir's own bookkeeping, so a failure left
// tens of megabytes behind on a shared runner on every invocation with nothing
// to show for it. A removal that fails usually means something still holds the
// tree -- a child process that outlived its test -- which is worth a red run in
// its own right. A run that already failed keeps its own exit code.
func cleanupSmokeRoot(dir string, stderr io.Writer, code int) int {
	if err := os.RemoveAll(dir); err != nil {
		_, _ = fmt.Fprintf(stderr, "smoke: could not remove the shared build root %s: %v\n", dir, err)

		if code == 0 {
			return 1
		}
	}

	return code
}

func buildBinary(t *testing.T) string {
	t.Helper()
	smoke.buildOnce.Do(func() {
		if smoke.baseTemp == "" {
			smoke.buildErr = fmt.Errorf("smoke temp root not initialized")
			return
		}
		bin := filepath.Join(smoke.baseTemp, "reusable-ci")
		cmd := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "./cmd/reusable-ci")
		cmd.Dir = repoRoot(t)
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		if err != nil {
			smoke.buildErr = fmt.Errorf("go build reusable-ci: %w\n%s", err, out)
			return
		}
		smoke.binary = bin
	})
	if smoke.buildErr != nil {
		t.Fatal(smoke.buildErr)
	}
	return smoke.binary
}

func repoRoot(t *testing.T) string {
	t.Helper()

	return filepath.Clean(filepath.Join(smoke.packageDir, "..", ".."))
}

// binaryRunTimeout bounds one child invocation. Every command in this tier is
// an offline CLI parse that finishes in milliseconds; the bound exists for the
// case where one does not.
const binaryRunTimeout = 30 * time.Second

// runBinary executes the built binary and returns its streams and exit code.
//
// The invocation is bounded. It used to be a plain exec.Command with no
// deadline, so a child that blocked -- waiting on a terminal, on a descriptor
// it inherited, on a provider call that should never have been reachable --
// took the whole package down at Go's ten-minute test timeout, and the report
// was a goroutine dump naming cmd.Wait rather than the invocation that hung.
// This tier exists to catch a command that misbehaves, and hanging is one of
// the ways it can.
//
// WaitDelay is set as well as the context: killing the child does not close
// output pipes that a grandchild inherited, and Wait blocks on the pipes, not
// on the process. Without it a bounded kill can still hang forever.
func runBinary(t *testing.T, bin string, args ...string) (string, string, int) {
	t.Helper()

	// t.Context() is cancelled when the test ends, so a child still running
	// then is killed with it rather than outliving the run.
	ctx, cancel := context.WithTimeout(t.Context(), binaryRunTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(),
		"HTTP_PROXY="+refusingProxy, "HTTPS_PROXY="+refusingProxy, "ALL_PROXY="+refusingProxy,
		"http_proxy="+refusingProxy, "https_proxy="+refusingProxy, "all_proxy="+refusingProxy,
		"NO_PROXY=", "no_proxy=")
	cmd.WaitDelay = 5 * time.Second

	// A test that chdirs chose its directory; one that did not would otherwise
	// run the child inside the source tree.
	if wd, err := os.Getwd(); err == nil && wd == smoke.packageDir {
		cmd.Dir = smokeWorkDir()
	}

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("%s %v did not finish within %s and was killed\nstdout: %q\nstderr: %q",
			bin, args, binaryRunTimeout, stdout.String(), stderr.String())
	}

	if cmd.Dir == smokeWorkDir() {
		requireSharedWorkDirEmpty(t, args)
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.String(), stderr.String(), exitErr.ExitCode()
	}

	if err != nil {
		t.Fatalf("run %s %v: %v", bin, args, err)
	}

	return stdout.String(), stderr.String(), 0
}

// requireSharedWorkDirEmpty fails when a child started in the shared working
// directory left anything in it, and clears it for the next run. A test that
// expects files chdirs to its own directory, and its children start there.
func requireSharedWorkDirEmpty(t *testing.T, args []string) {
	t.Helper()

	entries, err := os.ReadDir(smokeWorkDir())
	if err != nil {
		t.Fatalf("read the shared working directory: %v", err)
	}

	for _, entry := range entries {
		t.Errorf("%v left %q in the shared working directory", args, entry.Name())

		if removeErr := os.RemoveAll(filepath.Join(smokeWorkDir(), entry.Name())); removeErr != nil {
			t.Fatalf("remove %q: %v", entry.Name(), removeErr)
		}
	}
}

// TestBinary_VersionFlag asserts the bare-build default (`dev`): the shared
// binary is built without ldflags, so main.version/commit/date keep their
// package defaults.
func TestBinary_VersionFlag(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)

	stdout, stderr, code := runBinary(t, bin, "--version")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	// The whole line, not a substring of it. A search for "dev" is satisfied
	// by any output containing those three letters, and the line's SHAPE is
	// the part with consumers: it is what a support request quotes and what a
	// release check greps, so the binary name, the "version" keyword and both
	// parenthesised build fields all have to be there in order.
	//
	// The three values are the package defaults, which is what a build without
	// -ldflags produces. TestBinary_VersionMetadata_FromLdflags covers the
	// injected form; between them, a change to the format string fails in both
	// rather than in neither.
	if want := "reusable-ci version dev (commit none, built unknown)\n"; stdout != want {
		t.Errorf("--version stdout = %q, want %q", stdout, want)
	}

	if stderr != "" {
		t.Errorf("--version wrote to stderr: %q", stderr)
	}
}

// buildBinaryWithVersion builds a one-off binary with the same `-X main.*`
// ldflags the justfile and goreleaser inject, so the version-metadata wiring
// can be smoke-tested without the release toolchain (F8 / DIST-03).
func buildBinaryWithVersion(t *testing.T, version, commit, date string) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "reusable-ci-versioned")
	ldflags := fmt.Sprintf("-X main.version=%s -X main.commit=%s -X main.date=%s", version, commit, date)
	cmd := exec.Command("go", "build", "-buildvcs=false", "-ldflags", ldflags, "-o", bin, "./cmd/reusable-ci")
	cmd.Dir = repoRoot(t)
	cmd.Env = os.Environ()

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build (ldflags): %v\n%s", err, out)
	}

	return bin
}

// TestBinary_VersionMetadata_FromLdflags proves the `-X main.version/commit/date`
// wiring that both the justfile build recipes and goreleaser depend on — a bare
// `go build` reports `dev` and would mask drift here (F8).
func TestBinary_VersionMetadata_FromLdflags(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinaryWithVersion(t, "v9.9.9", "abc1234def0", "2026-01-02T03:04:05Z")

	stdout, _, code := runBinary(t, bin, "--version")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	// Same line, same order, with the injected values in place of the
	// defaults. Checking the three values as substrings could not tell a
	// correctly assembled line from three values printed in any arrangement.
	if want := "reusable-ci version v9.9.9 (commit abc1234def0, built 2026-01-02T03:04:05Z)\n"; stdout != want {
		t.Errorf("--version stdout = %q, want %q", stdout, want)
	}
}

func TestBinary_WorkflowInputDefaults_Success(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join(".github", "workflows", "ok.yml"), []byte("name: ok\non:\n  workflow_call:\n    inputs:\n      foo:\n        default: literal\n"))
	_, stderr, code := runBinary(t, bin, "validate", "workflow", "input-defaults", "--root", fsys.Root)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr)
	}
	if strings.TrimSpace(stderr) != "Workflow input defaults look valid." {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestBinary_WorkflowInputDefaults_InvalidDefaultExitsWithValidation(t *testing.T) {
	// GITHUB_ACTIONS=true → the runner is detected as GitHub, so the
	// annotation renders as a `::error file=…::` workflow command (the form
	// the reusable workflows actually run under). Off-GitHub it renders a
	// plain `Error: <file>:<line>:` line — see the sibling test below.
	env := testenv.New(t)
	env.Setenv("GITHUB_ACTIONS", "true")
	bin := buildBinary(t)
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join(".github", "workflows", "bad.yml"), []byte(strings.Join([]string{
		"name: bad",
		"on:",
		"  workflow_call:",
		"    inputs:",
		"      foo:",
		"        default: ${{ github.ref_name }}",
	}, "\n")+"\n"))
	_, stderr, code := runBinary(t, bin, "validate", "workflow", "input-defaults", "--root", fsys.Root)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "::error file=.github/workflows/bad.yml,line=6::workflow_call input defaults must be literal values") {
		t.Errorf("stderr = %q", stderr)
	}
}

// TestBinary_WorkflowInputDefaults_PlainOnNonGitHubRunner proves the F2
// behaviour: off a GitHub runner the same gate renders a readable, plain
// location-prefixed line with no `::error::` workflow-command noise.
func TestBinary_WorkflowInputDefaults_PlainOnNonGitHubRunner(t *testing.T) {
	_ = testenv.New(t) // no GITHUB_ACTIONS → local runner → plain format
	bin := buildBinary(t)
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join(".github", "workflows", "bad.yml"), []byte(strings.Join([]string{
		"name: bad", "on:", "  workflow_call:", "    inputs:", "      foo:",
		"        default: ${{ github.ref_name }}",
	}, "\n")+"\n"))
	_, stderr, code := runBinary(t, bin, "validate", "workflow", "input-defaults", "--root", fsys.Root)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr=%q", code, stderr)
	}
	if strings.Contains(stderr, "::error") {
		t.Errorf("non-GitHub runner must not emit workflow-command syntax; got %q", stderr)
	}
	if !strings.Contains(stderr, "Error: .github/workflows/bad.yml:6:") {
		t.Errorf("expected plain location-prefixed error; got %q", stderr)
	}
}

func TestBinary_ReleaseResolveMetadata_WritesGitHubOutput(t *testing.T) {
	env := testenv.New(t)
	fsys := testfs.NewReal(t)
	outputPath := fsys.WriteFile("github_output", nil)
	env.Setenv("GITHUB_ACTIONS", "true")
	env.Setenv("GITHUB_OUTPUT", outputPath)
	env.Setenv("VERSION", "v1.2.3")
	env.Setenv("REPOSITORY", "diggsweden/reusable-ci")
	env.Setenv("ARTIFACT_NAME", "artifact-name")

	bin := buildBinary(t)
	_, stderr, code := runBinary(t, bin, "release", "resolve", "metadata")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr)
	}
	// The output file is parsed, not searched: every line must be a unique
	// key=value record, and the set must be exactly these three. A substring
	// check passed with a duplicated key, a stray extra output, or
	// version=v1.2.3 embedded in some other line.
	body := strings.TrimSuffix(string(fsys.ReadFile("github_output")), "\n")

	lines := strings.Split(body, "\n")
	keys := make([]string, 0, len(lines))
	records := make(map[string]string, len(lines))

	for _, line := range lines {
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" {
			t.Fatalf("output line %q is not a key=value record:\n%s", line, body)
		}

		if _, dup := records[key]; dup {
			t.Errorf("output %q written twice:\n%s", key, body)
		}

		keys = append(keys, key)
		records[key] = value
	}

	want := map[string]string{"version": "v1.2.3", "version-no-v": "1.2.3", "project-name": "artifact-name"}
	if len(records) != len(want) || records["version"] != want["version"] || records["version-no-v"] != want["version-no-v"] || records["project-name"] != want["project-name"] {
		t.Errorf("outputs = %v (order %q), want exactly %v", records, keys, want)
	}
}

// TestBinary_SummaryQualityCheckStatus_WritesStepSummary passes concrete
// records through argv, one per outcome, and compares the whole summary. It
// ran with no arguments, which renders only the headers and the vacuous pass,
// so a binary that ignored its records or mapped them wrongly passed.
func TestBinary_SummaryQualityCheckStatus_WritesStepSummary(t *testing.T) {
	env := testenv.New(t)
	fsys := testfs.NewReal(t)
	summaryPath := fsys.WriteFile("summary.md", nil)
	env.Setenv("GITHUB_ACTIONS", "true")
	env.Setenv("GITHUB_STEP_SUMMARY", summaryPath)

	bin := buildBinary(t)

	_, stderr, code := runBinary(t, bin, "report", "status", "quality-check",
		"Lint|true|success", "SAST|false|skipped", "Dependencies|true|skipped", "Secrets|true|failure")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (the collector never fails the job); stderr=%q", code, stderr)
	}

	const want = "## Pull Request Check Status\n\n" +
		"### Quality Check Results\n" +
		"| Check | Status |\n" +
		"|-------|--------|\n" +
		"| Lint | ✓ Pass |\n" +
		"| SAST | 🔸 Disabled |\n" +
		"| Dependencies | − Skipped |\n" +
		"| Secrets | ✗ Fail |\n\n" +
		"### ✗ Some checks failed\n" +
		"Please review the failures above and fix any issues.\n" +
		"Note: Individual linter failures are shown above. This status job always succeeds to provide summary.\n"

	if body := string(fsys.ReadFile("summary.md")); !strings.HasPrefix(body, want) || strings.TrimSpace(strings.TrimPrefix(body, want)) != "" {
		t.Errorf("summary =\n%s\nwant\n%s", body, want)
	}
}

func TestBinary_ValidateAuthRegistry_MissingPasswordExitsNoPerm(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("USE_CI_TOKEN", "false")
	env.Setenv("REGISTRY", "registry.example.com")
	env.Setenv("CI_REGISTRY", "ghcr.io")
	env.Setenv("REGISTRY_PASSWORD", "")

	bin := buildBinary(t)
	_, stderr, code := runBinary(t, bin, "validate", "auth", "registry")
	if code != 77 {
		t.Fatalf("exit code = %d, want 77; stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "registry-password secret is required") {
		t.Errorf("stderr = %q", stderr)
	}
}

// TestBinary_ValidateAuthRegistry_SecretIsUsedButNeverWritten is the positive
// control for the refusal above. A nonempty password or token in the
// environment passes the check, and the secret appears in no stdout, stderr,
// step output, step summary or file under the run's home or working directory.
// A blank one is refused like a missing one.
func TestBinary_ValidateAuthRegistry_SecretIsUsedButNeverWritten(t *testing.T) {
	const secret = "synthetic-registry-secret-6f1d"

	bin := buildBinary(t)

	for _, tc := range []struct {
		name, variable, value string
		wantCode              int
	}{
		{"password", "REGISTRY_PASSWORD", secret, 0},
		{"token", "REGISTRY_TOKEN", secret, 0},
		{"blank password", "REGISTRY_PASSWORD", " \t", 77},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := testenv.New(t)
			fsys := testfs.NewReal(t)
			fsys.Chdir()

			env.Setenv("USE_CI_TOKEN", "false")
			env.Setenv("REGISTRY", "registry.example.com")
			env.Setenv("CI_REGISTRY", "ghcr.io")
			env.Setenv("REGISTRY_PASSWORD", "")
			env.Setenv("REGISTRY_TOKEN", "")
			env.Setenv(tc.variable, tc.value)
			env.Setenv("GITHUB_OUTPUT", fsys.WriteFile("output", nil))
			env.Setenv("GITHUB_STEP_SUMMARY", fsys.WriteFile("summary.md", nil))

			stdout, stderr, code := runBinary(t, bin, "validate", "auth", "registry")
			if code != tc.wantCode {
				t.Fatalf("exit code = %d, want %d; stderr=%q", code, tc.wantCode, stderr)
			}

			if strings.Contains(stdout+stderr, secret) {
				t.Errorf("secret in streams: stdout=%q stderr=%q", stdout, stderr)
			}

			for _, dir := range []string{fsys.Root, env.Home} {
				root, err := os.OpenRoot(dir)
				if err != nil {
					t.Fatal(err)
				}

				_ = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
					if err != nil || !entry.Type().IsRegular() {
						return nil //nolint:nilerr // an unreadable entry cannot hold the secret we wrote nowhere.
					}

					if body, readErr := fs.ReadFile(root.FS(), path); readErr == nil && bytes.Contains(body, []byte(secret)) {
						t.Errorf("secret written to %s", filepath.Join(dir, path))
					}

					return nil
				})

				_ = root.Close()
			}
		})
	}
}

func TestBinary_Doctor_OKWhenMinimalRepoIsClean(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)
	fsys := testfs.NewReal(t)

	// Minimal compliant repo: artifacts.yml with a meta artifact,
	// no sign block (defaults to gpg, no extra requirements).
	fsys.WriteFile(filepath.Join(".reusable-ci", "artifacts.yml"), []byte(
		"artifacts:\n  - name: x\n    project-type: meta\n",
	))

	stdout, stderr, code := runBinary(t, bin, "doctor", "--root", fsys.Root)
	if code != 0 {
		t.Fatalf("exit %d (want 0)\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	if !strings.Contains(stdout, "[OK] artifacts.yml present") {
		t.Errorf("stdout missing OK marker; got %s", stdout)
	}

	if strings.Contains(stdout, "[FAIL]") {
		t.Errorf("clean repo must produce no FAIL checks; got %s", stdout)
	}
}

func TestBinary_Doctor_FailWhenSigstoreLacksIDToken(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)
	fsys := testfs.NewReal(t)

	// sigstore method but no workflow with id-token: write.
	fsys.WriteFile(filepath.Join(".reusable-ci", "artifacts.yml"), []byte(
		"artifacts:\n  - name: x\n    project-type: meta\nsign:\n  method: sigstore\n",
	))

	stdout, _, code := runBinary(t, bin, "doctor", "--root", fsys.Root)

	// ExitCodeValidation = 1 (POSIX-style rule failure).
	if code != 1 {
		t.Errorf("exit = %d, want 1 (ExitCodeValidation)\nstdout: %s", code, stdout)
	}

	if !strings.Contains(stdout, "[FAIL] workflow id-token permission") {
		t.Errorf("output missing expected FAIL line; got %s", stdout)
	}

	if !strings.Contains(stdout, "id-token: write") {
		t.Errorf("remediation must mention id-token: write; got %s", stdout)
	}
}

func TestBinary_Doctor_MissingArtifactsYMLFails(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)
	fsys := testfs.NewReal(t)
	// Don't create .reusable-ci/artifacts.yml — that's the point.

	stdout, _, code := runBinary(t, bin, "doctor", "--root", fsys.Root)
	if code != 1 {
		t.Errorf("missing artifacts.yml: exit = %d, want 1 (ExitCodeValidation); got %s", code, stdout)
	}

	if !strings.Contains(stdout, "[FAIL] artifacts.yml present") {
		t.Errorf("output missing artifacts-not-found FAIL; got %s", stdout)
	}
}

func TestBinary_Doctor_ArtifactsFlagOverride(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)
	fsys := testfs.NewReal(t)

	// Put the artifacts.yml in a non-standard location and point
	// --artifacts at it.
	customPath := filepath.Join(fsys.Root, "custom-artifacts.yml")
	if err := os.WriteFile(customPath, []byte(
		"artifacts:\n  - name: x\n    project-type: meta\n",
	), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	stdout, stderr, code := runBinary(t, bin, "doctor", "--root", fsys.Root, "--artifacts-file", customPath)
	if code != 0 {
		t.Fatalf("exit %d with --artifacts-file override\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	if !strings.Contains(stdout, customPath) {
		t.Errorf("output should reference the custom path %q; got %s", customPath, stdout)
	}
}

func TestBinary_ValidateTagFormat_InvalidTagExitsValidation(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)
	_, stderr, code := runBinary(t, bin, "validate", "tag", "format", "--tag", "1.0.0")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "invalid tag format") {
		t.Errorf("stderr = %q", stderr)
	}
}

// TestCleanupSmokeRoot_ReportsAndFailsOnARemovalError drives the helper with a
// tree that cannot be removed: a directory whose parent denies writes, which
// is owned temporary state rather than anything on the host.
func TestCleanupSmokeRoot_ReportsAndFailsOnARemovalError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory modes do not deny removal, so the failure cannot be produced")
	}

	root := t.TempDir()
	stuck := filepath.Join(root, "build")

	if err := os.MkdirAll(filepath.Join(stuck, "inner"), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(stuck, "inner", "reusable-ci"), []byte("binary"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Removing inner/reusable-ci needs write on inner; take it away.
	if err := os.Chmod(filepath.Join(stuck, "inner"), 0o500); err != nil { //nolint:gosec // G302: deliberately unwritable to force the failure.
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(filepath.Join(stuck, "inner"), 0o700) }) //nolint:gosec // restore so t.TempDir can clean up.

	for _, tc := range []struct {
		name     string
		in, want int
	}{
		{name: "a passing run turns red", in: 0, want: 1},
		{name: "a failing run keeps its code", in: 3, want: 3},
	} {
		var stderr bytes.Buffer

		if got := cleanupSmokeRoot(stuck, &stderr, tc.in); got != tc.want {
			t.Errorf("%s: exit code = %d, want %d", tc.name, got, tc.want)
		}

		if !strings.Contains(stderr.String(), stuck) {
			t.Errorf("%s: the failure was not reported with the path:\n%s", tc.name, stderr.String())
		}
	}

	// And a removable tree is silent and leaves the code alone.
	clean := filepath.Join(root, "clean")
	if err := os.MkdirAll(clean, 0o750); err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer
	if got := cleanupSmokeRoot(clean, &stderr, 0); got != 0 || stderr.Len() != 0 {
		t.Errorf("clean removal: code = %d, stderr = %q, want 0 and nothing", got, stderr.String())
	}
}

// TestBinary_Doctor_JSONReportIsConsistent decodes the machine-readable
// report for a repository with exactly one failing check. The text tests look
// for single markers; here every check name must be unique, the failure count
// must equal the checks marked fail, only failing checks carry a remediation,
// and stdout must hold nothing but the one document.
func TestBinary_Doctor_JSONReportIsConsistent(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)
	fsys := testfs.NewReal(t)

	fsys.WriteFile(filepath.Join(".reusable-ci", "artifacts.yml"), []byte(
		"artifacts:\n  - name: x\n    project-type: meta\nsign:\n  method: sigstore\n",
	))

	stdout, stderr, code := runBinary(t, bin, "--json", "doctor", "--root", fsys.Root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	var report struct {
		Checks []struct {
			Name        string `json:"name"`
			Severity    string `json:"severity"`
			Message     string `json:"message"`
			Remediation string `json:"remediation"`
		} `json:"checks"`
		Failures int `json:"failures"`
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}

	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	failing := []string{}

	for _, check := range report.Checks {
		if seen[check.Name] {
			t.Errorf("check %q reported twice", check.Name)
		}

		seen[check.Name] = true

		switch check.Severity {
		case "fail":
			failing = append(failing, check.Name)

			if check.Remediation == "" {
				t.Errorf("failing check %q has no remediation", check.Name)
			}
		case "ok":
			if check.Remediation != "" {
				t.Errorf("passing check %q carries a remediation %q", check.Name, check.Remediation)
			}
		}
	}

	if report.Failures != len(failing) || len(failing) != 1 || failing[0] != "workflow id-token permission" {
		t.Errorf("failures = %d, failing checks = %q; want the count to match exactly the id-token check", report.Failures, failing)
	}

	if !strings.Contains(stderr, "doctor: 1 failing check(s)") {
		t.Errorf("stderr = %q, want the failure count", stderr)
	}
}
