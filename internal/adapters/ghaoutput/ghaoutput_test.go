// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package ghaoutput_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/adapters/ghaoutput"
	"github.com/diggsweden/reusable-ci/internal/testutil/testenv"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestSet_WritesScalar(t *testing.T) {
	fsys := testfs.NewReal(t)
	path := fsys.WriteFile("out", nil)

	s := ghaoutput.New(path)
	if err := s.Set(context.Background(), "version", "1.2.3"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	data := fsys.ReadFile("out")
	if got := string(data); got != "version=1.2.3\n" {
		t.Errorf("output = %q, want %q", got, "version=1.2.3\n")
	}
}

func TestSetMultiline_HeredocShape(t *testing.T) {
	fsys := testfs.NewReal(t)
	path := fsys.WriteFile("out", nil)

	s := ghaoutput.New(path)
	err := s.SetMultiline(context.Background(), "tags", []string{
		"ghcr.io/x/y:1.2.3", "ghcr.io/x/y:1.2", "ghcr.io/x/y:1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	data := fsys.ReadFile("out")
	got := string(data)

	if !strings.HasPrefix(got, "tags<<EOF_") {
		t.Errorf("output does not start with heredoc opener: %q", got)
	}
	if !strings.Contains(got, "ghcr.io/x/y:1.2.3\nghcr.io/x/y:1.2\nghcr.io/x/y:1\n") {
		t.Errorf("output missing tag lines: %q", got)
	}
	// Delimiter appears twice: once as opener, once as closer
	lines := strings.Split(got, "\n")
	if len(lines) < 2 {
		t.Fatalf("not enough lines: %d", len(lines))
	}
	delim := strings.TrimPrefix(lines[0], "tags<<")
	if delim == "" || !strings.HasPrefix(delim, "EOF_") {
		t.Errorf("delimiter missing or malformed: %q", delim)
	}
	closingFound := false
	for _, l := range lines[1:] {
		if l == delim {
			closingFound = true
			break
		}
	}
	if !closingFound {
		t.Errorf("closing delimiter not found in output: %q", got)
	}
}

func TestSetAfterClose_Errors(t *testing.T) {
	fsys := testfs.NewReal(t)
	path := fsys.WriteFile("out", nil)

	s := ghaoutput.New(path)
	require.NoError(t, s.Close(context.Background()))
	if err := s.Set(context.Background(), "k", "v"); err == nil {
		t.Errorf("Set after Close did not error")
	}
	if err := s.SetMultiline(context.Background(), "k", []string{"x"}); err == nil {
		t.Errorf("SetMultiline after Close did not error")
	}
}

func TestNewFromEnv(t *testing.T) {
	tests := []struct {
		name       string
		githubPath string
		ciPath     string
		wantFile   string
		wantEmpty  string
	}{
		{name: "prefers_github_output", githubPath: "gh", ciPath: "ci", wantFile: "gh", wantEmpty: "ci"},
		{name: "falls_back_to_ci_output", ciPath: "ci", wantFile: "ci"},
		{name: "devnull_when_neither"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			env := testenv.New(t)
			if testCase.githubPath != "" {
				env.Setenv("GITHUB_OUTPUT", fsys.WriteFile(testCase.githubPath, nil))
			} else {
				env.Setenv("GITHUB_OUTPUT", "")
			}
			if testCase.ciPath != "" {
				env.Setenv("CI_OUTPUT", fsys.WriteFile(testCase.ciPath, nil))
			} else {
				env.Setenv("CI_OUTPUT", "")
			}

			s := ghaoutput.NewFromEnv()
			require.NoError(t, s.Set(context.Background(), "k", "v"))
			require.NoError(t, s.Close(context.Background()))

			if testCase.wantFile != "" {
				if data := fsys.ReadFile(testCase.wantFile); !strings.Contains(string(data), "k=v") {
					t.Errorf("%s not used: %q", testCase.wantFile, data)
				}
			}
			if testCase.wantEmpty != "" {
				if data := fsys.ReadFile(testCase.wantEmpty); strings.Contains(string(data), "k=v") {
					t.Errorf("%s was used unexpectedly: %q", testCase.wantEmpty, data)
				}
			}
		})
	}
}
