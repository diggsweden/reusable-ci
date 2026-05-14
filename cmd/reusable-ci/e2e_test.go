//go:build e2e

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

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

	"github.com/diggsweden/reusable-ci/internal/testutil/testenv"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
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
	stdout, _, code := runBinary(t, bin, "validate", "workflow-input-defaults", "--root", fsys.Root)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; output=%q", code, stdout)
	}
	if strings.TrimSpace(stdout) != "Workflow input defaults look valid." {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestBinary_WorkflowInputDefaults_InvalidDefaultExitsWithValidation(t *testing.T) {
	_ = testenv.New(t)
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
	stdout, _, code := runBinary(t, bin, "validate", "workflow-input-defaults", "--root", fsys.Root)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; output=%q", code, stdout)
	}
	if !strings.Contains(stdout, "::error file=.github/workflows/bad.yml,line=6::workflow_call input defaults must be literal values") {
		t.Errorf("stdout = %q", stdout)
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
	_, stderr, code := runBinary(t, bin, "release", "resolve-release-metadata")
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
	_, stderr, code := runBinary(t, bin, "summary", "quality-check-status")
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

func TestBinary_PublishValidateAuth_MissingPasswordExitsNoPerm(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("USE_CI_TOKEN", "false")
	env.Setenv("TARGET_REGISTRY", "registry.example.com")
	env.Setenv("CI_REGISTRY", "ghcr.io")
	env.Setenv("REGISTRY_PASSWORD", "")

	bin := buildBinary(t)
	_, stderr, code := runBinary(t, bin, "publish", "validate-auth")
	if code != 77 {
		t.Fatalf("exit code = %d, want 77; stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "registry-password secret is required") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestBinary_ValidateRefType_NonTagExitsValidation(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)
	_, stderr, code := runBinary(t, bin, "validate", "ref-type", "branch", "main", "refs/heads/main")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "Release workflow must be triggered by pushing a tag") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestBinary_ValidateTagFormat_InvalidTagExitsValidation(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)
	_, stderr, code := runBinary(t, bin, "validate", "tag-format", "1.0.0")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "Invalid tag format") {
		t.Errorf("stderr = %q", stderr)
	}
}
