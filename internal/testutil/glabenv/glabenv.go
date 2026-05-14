// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package glabenv sets up GitLab-CI-style environment for tests.
//
// GitLab has no per-step key=value sink equivalent to $GITHUB_OUTPUT;
// instead pipelines surface scalar outputs via `artifacts:reports:dotenv`
// (a file the script writes that GitLab parses as KEY=VALUE pairs).
//
// This helper:
//   - exports the canonical CI_* env vars (GITLAB_CI=true, CI_COMMIT_*,
//     CI_PROJECT_*, CI_PIPELINE_*) with sane defaults the test can override
//   - allocates a tempfile and points CI_OUTPUT at it (the platform-portable
//     name; the GitLab adapter writes dotenv format here)
//   - exposes Output(key) for scalar reads from the dotenv file
//
// Multi-line outputs do not have a stable GitLab equivalent — tests that
// need them should write through ManifestSink instead and read the
// resulting JSON file.
package glabenv

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/testutil/testenv"
)

// Env wraps the CI_OUTPUT tempfile path for tests to inspect.
type Env struct {
	t          *testing.T
	OutputPath string
}

// Setenv forwards to the underlying test within the already-isolated env.
func (e *Env) Setenv(key, value string) {
	e.t.Helper()
	e.t.Setenv(key, value)
}

// Setup exports the CI_* env vars expected by the GitLab platform branch
// of internal/platform.Detect(), creates an empty CI_OUTPUT dotenv file,
// and returns a handle for reading it.
func Setup(t *testing.T) *Env {
	t.Helper()
	env := testenv.New(t)
	dir := env.MkdirAll("glabenv")

	outPath := filepath.Join(dir, "output.env")
	if err := os.WriteFile(outPath, nil, 0o644); err != nil {
		t.Fatalf("glabenv: create output: %v", err)
	}

	env.Setenv("GITLAB_CI", "true")
	env.Setenv("CI_OUTPUT", outPath)

	// Sane default context. Tests override individual values via t.Setenv.
	defaults := map[string]string{
		"CI_COMMIT_SHA":       "abcdef0123456789abcdef0123456789abcdef01",
		"CI_COMMIT_SHORT_SHA": "abcdef0",
		"CI_COMMIT_REF_NAME":  "main",
		"CI_COMMIT_TAG":       "",
		"CI_COMMIT_BRANCH":    "main",
		"CI_PROJECT_PATH":     "example/project",
		"CI_PROJECT_URL":      "https://gitlab.com/example/project",
		"CI_PIPELINE_ID":      "100",
		"CI_PIPELINE_URL":     "https://gitlab.com/example/project/-/pipelines/100",
		"CI_PIPELINE_SOURCE":  "push",
		"CI_SERVER_URL":       "https://gitlab.com",
		"GITLAB_USER_LOGIN":   "test-bot",
	}
	for k, v := range defaults {
		env.Setenv(k, v)
	}

	return &Env{t: t, OutputPath: outPath}
}

// Output reads the scalar value for `key` from the dotenv file.
// Returns empty string if the key is not present.
func (e *Env) Output(key string) string {
	e.t.Helper()
	f, err := os.Open(e.OutputPath)
	if err != nil {
		e.t.Fatalf("glabenv: open output: %v", err)
	}
	defer func() { _ = f.Close() }()

	prefix := key + "="
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	if err := sc.Err(); err != nil {
		e.t.Fatalf("glabenv: scan: %v", err)
	}
	return ""
}

// SetTagRef configures the env to look like a tag push (used by tests
// that exercise the ref/event=tag tag-rule case).
func (e *Env) SetTagRef(name string) {
	e.t.Helper()
	e.t.Setenv("CI_COMMIT_TAG", name)
	e.t.Setenv("CI_COMMIT_REF_NAME", name)
	e.t.Setenv("CI_COMMIT_BRANCH", "")
}

// SetMergeRequest configures the env to look like a merge-request pipeline.
func (e *Env) SetMergeRequest(iid, sourceBranch, targetBranch string) {
	e.t.Helper()
	e.t.Setenv("CI_MERGE_REQUEST_IID", iid)
	e.t.Setenv("CI_MERGE_REQUEST_SOURCE_BRANCH_NAME", sourceBranch)
	e.t.Setenv("CI_MERGE_REQUEST_TARGET_BRANCH_NAME", targetBranch)
	e.t.Setenv("CI_PIPELINE_SOURCE", "merge_request_event")
	e.t.Setenv("CI_COMMIT_REF_NAME", sourceBranch)
}
