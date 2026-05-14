// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package stepsummary_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/adapters/stepsummary"
	"github.com/diggsweden/reusable-ci/internal/testutil/testenv"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestSink_AppendsMarkdown(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	path := fsys.Path("summary.md")
	s := stepsummary.New(path)
	if err := s.Append(context.Background(), "## title\n"); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(context.Background(), "second line\n"); err != nil {
		t.Fatal(err)
	}
	data := fsys.ReadFile("summary.md")
	if !strings.Contains(string(data), "## title") || !strings.Contains(string(data), "second line") {
		t.Errorf("file content = %q", data)
	}
}

func TestSink_NoopWhenEmptyPath(t *testing.T) {
	t.Parallel()
	s := stepsummary.New("")
	if err := s.Append(context.Background(), "would-have-written"); err != nil {
		t.Errorf("empty-path Append should be a no-op, got %v", err)
	}
}

func TestNewFromEnv_PrefersGitHubStepSummary(t *testing.T) {
	fsys := testfs.NewReal(t)
	gh := fsys.Path("gh.md")
	gl := fsys.Path("gl.md")
	env := testenv.New(t)
	env.Setenv("GITHUB_STEP_SUMMARY", gh)
	env.Setenv("CI_SUMMARY_FILE", gl)
	if err := stepsummary.NewFromEnv().Append(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(gh); err != nil {
		t.Errorf("GH path not written: %v", err)
	}
	if _, err := os.Stat(gl); err == nil {
		t.Errorf("GitLab path should not have been written")
	}
}
