//go:build e2e

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

var (
	sharedBinaryOnce sync.Once
	sharedBinaryPath string
	sharedBinaryErr  error
	e2eBaseTemp      string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "reusable-ci-e2e-*")
	if err != nil {
		panic(err)
	}
	e2eBaseTemp = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func buildBinary(t *testing.T) string {
	t.Helper()
	sharedBinaryOnce.Do(func() {
		if e2eBaseTemp == "" {
			sharedBinaryErr = fmt.Errorf("e2e temp root not initialized")
			return
		}
		bin := filepath.Join(e2eBaseTemp, "reusable-ci")
		cmd := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "./cmd/reusable-ci")
		cmd.Dir = repoRoot(t)
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		if err != nil {
			sharedBinaryErr = fmt.Errorf("go build reusable-ci: %w\n%s", err, out)
			return
		}
		sharedBinaryPath = bin
	})
	if sharedBinaryErr != nil {
		t.Fatal(sharedBinaryErr)
	}
	return sharedBinaryPath
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func runBinary(t *testing.T, bin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		return stdout.String(), stderr.String(), ee.ExitCode()
	}
	if err != nil {
		t.Fatalf("run %s %v: %v", bin, args, err)
	}
	return stdout.String(), stderr.String(), 0
}

// TestBinary_VersionFlag asserts the bare-build default (`dev`): the shared
// binary is built without ldflags, so main.version/commit/date keep their
// package defaults.
func TestBinary_VersionFlag(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)
	stdout, _, code := runBinary(t, bin, "--version")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "dev") {
		t.Errorf("stdout = %q, want version output", stdout)
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

	for _, want := range []string{"v9.9.9", "abc1234def0", "2026-01-02T03:04:05Z"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("--version output %q missing injected %q", stdout, want)
		}
	}
}

func TestBinary_InvalidLogLevelExitsWithUsage(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)
	_, stderr, code := runBinary(t, bin, "--log-level=bogus")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "invalid log-level") {
		t.Errorf("stderr = %q", stderr)
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
	body := string(fsys.ReadFile("github_output"))
	for _, want := range []string{"version=v1.2.3", "version-no-v=1.2.3", "project-name=artifact-name"} {
		if !strings.Contains(body, want) {
			t.Errorf("output file missing %q:\n%s", want, body)
		}
	}
}

func TestBinary_SummaryQualityCheckStatus_WritesStepSummary(t *testing.T) {
	env := testenv.New(t)
	fsys := testfs.NewReal(t)
	summaryPath := fsys.WriteFile("summary.md", nil)
	env.Setenv("GITHUB_ACTIONS", "true")
	env.Setenv("GITHUB_STEP_SUMMARY", summaryPath)

	bin := buildBinary(t)
	_, stderr, code := runBinary(t, bin, "report", "status", "quality-check")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr)
	}
	body := string(fsys.ReadFile("summary.md"))
	for _, want := range []string{"Pull Request Check Status", "Quality Check Results"} {
		if !strings.Contains(body, want) {
			t.Errorf("summary missing %q:\n%s", want, body)
		}
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

// The `validate ref-type` subcommand this once exercised no longer exists: the
// check moved into `validate prerequisites`, which on a local platform refuses at
// token validation before reaching it, so there is no single-command surface to
// drive from a black box any more.
//
// Deleted rather than rewritten. Driving prerequisites far enough to reach the
// ref-type check needs a forge, which makes it a live-tier concern, not an e2e
// one -- and the claim itself is asserted where it belongs:
// internal/domain/validate/reftype_test.go on the error, and
// internal/app/validate/refs_test.go on the use case.
//
// It had been failing for weeks on "flag provided but not defined: -ref-type",
// because nothing ran this tier.

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
