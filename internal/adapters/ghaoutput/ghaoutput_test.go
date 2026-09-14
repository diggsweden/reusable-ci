// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package ghaoutput_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ghaoutput"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
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

func TestSet_RejectsNewlineValue(t *testing.T) {
	fsys := testfs.NewReal(t)

	// Both, and a bare carriage return: the file is line-oriented, so a
	// value carrying either would forge further key=value entries. A lone
	// \r is the one that arrives by accident, from a CRLF-checked-out
	// input file rather than from an attacker.
	for _, value := range []string{"one\ntwo", "one\rtwo", "one\r\ntwo", "trailing\n"} {
		s := ghaoutput.New(fsys.WriteFile("out", nil))

		err := s.Set(context.Background(), "bad", value)
		if !errors.Is(err, errs.ErrValidation) {
			t.Errorf("value %q: err = %v, want ErrValidation", value, err)
		}
	}
}

// TestSetMultiline_DelimiterIsUnpredictable covers the property the
// heredoc rests on. Lines are written verbatim, so whatever terminates
// the block must be something their author cannot know: a line equal to
// the delimiter would close the heredoc early and everything after it
// would be read by the runner as further outputs.
//
// A fixed delimiter would satisfy every other test in this file.
func TestSetMultiline_DelimiterIsUnpredictable(t *testing.T) {
	fsys := testfs.NewReal(t)

	seen := map[string]bool{}

	for i := range 20 {
		path := fsys.WriteFile(fmt.Sprintf("out-%d", i), nil)

		s := ghaoutput.New(path)
		if err := s.SetMultiline(context.Background(), "tags", []string{"a"}); err != nil {
			t.Fatal(err)
		}

		if err := s.Close(context.Background()); err != nil {
			t.Fatal(err)
		}

		first, _, _ := strings.Cut(string(fsys.ReadFile(fmt.Sprintf("out-%d", i))), "\n")

		delim := strings.TrimPrefix(first, "tags<<")
		if delim == first {
			t.Fatalf("no heredoc opener: %q", first)
		}

		// 16 random bytes, hex-encoded, behind the EOF_ marker.
		if len(delim) != len("EOF_")+32 {
			t.Errorf("delimiter %q is %d chars, want %d", delim, len(delim), len("EOF_")+32)
		}

		if seen[delim] {
			t.Fatalf("delimiter %q reused across calls", delim)
		}

		seen[delim] = true
	}
}

// TestSetMultiline_ContentCannotCloseTheHeredoc feeds lines that would
// close a fixed or guessable delimiter. They must appear as content.
func TestSetMultiline_ContentCannotCloseTheHeredoc(t *testing.T) {
	fsys := testfs.NewReal(t)
	path := fsys.WriteFile("out", nil)

	lines := []string{"EOF", "EOF_", "ghcr.io/x/y:1.2.3", "injected=true"}

	s := ghaoutput.New(path)
	if err := s.SetMultiline(context.Background(), "tags", lines); err != nil {
		t.Fatal(err)
	}

	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	got := string(fsys.ReadFile("out"))

	first, _, _ := strings.Cut(got, "\n")

	delim := strings.TrimPrefix(first, "tags<<")
	if delim == first {
		t.Fatalf("no heredoc opener: %q", first)
	}

	// The whole file: opener, every line verbatim, the delimiter, nothing
	// after it. Anything the runner would read as a further output shows up
	// here as a difference.
	want := "tags<<" + delim + "\n" + strings.Join(lines, "\n") + "\n" + delim + "\n"
	if got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestSet_RejectsInvalidOutputKey(t *testing.T) {
	fsys := testfs.NewReal(t)
	s := ghaoutput.New(fsys.WriteFile("out", nil)) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	for _, key := range []string{"", "1bad", "bad key", "bad=value", "bad\nkey", "bad\rkey", "bad<<EOF"} {
		if err := s.Set(context.Background(), key, "value"); !errors.Is(err, errs.ErrValidation) {
			t.Errorf("key %q: Set err = %v, want ErrValidation", key, err)
		}

		if err := s.SetMultiline(context.Background(), key, []string{"value"}); !errors.Is(err, errs.ErrValidation) {
			t.Errorf("key %q: SetMultiline err = %v, want ErrValidation", key, err)
		}
	}
}

func TestSetAfterClose_Errors(t *testing.T) {
	fsys := testfs.NewReal(t)
	path := fsys.WriteFile("out", nil)

	s := ghaoutput.New(path) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	require.NoError(t, s.Close(context.Background()))

	if err := s.Set(context.Background(), "k", "v"); err == nil {
		t.Errorf("Set after Close did not error")
	}

	if err := s.SetMultiline(context.Background(), "k", []string{"x"}); err == nil {
		t.Errorf("SetMultiline after Close did not error")
	}

	// The refusal has to mean nothing was appended: the runner reads this
	// file after the step ends, so a write that errored but landed anyway
	// would still become an output.
	if data := fsys.ReadFile("out"); len(data) != 0 {
		t.Errorf("output written after Close: %q", data)
	}
}

func TestNewFromEnv_PrefersGitHubOutputAndFallsBackToDevNull(t *testing.T) {
	tests := []struct {
		name       string
		githubPath string
		ciPath     string
		wantFile   string
		wantEmpty  string
	}{
		{name: "uses_github_output", githubPath: "gh", ciPath: "ci", wantFile: "gh", wantEmpty: "ci"},
		{name: "ignores_ci_output", ciPath: "ci", wantEmpty: "ci"},
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

// TestNewFromEnvVars_PrefersTheFirstSetName covers the constructor the CLI
// actually wires up (deps.go passes "FORGEJO_OUTPUT", "GITHUB_OUTPUT"), which
// had no test at all. Order is the whole contract: on a Forgejo runner both
// names are set — the native one and the compat alias — and writing to the
// alias would put the step's outputs where the runner does not read them.
func TestNewFromEnvVars_PrefersTheFirstSetName(t *testing.T) {
	tests := []struct {
		name     string
		forgejo  string
		github   string
		wantFile string
		wantIdle string
	}{
		{name: "both set, native wins", forgejo: "fj", github: "gh", wantFile: "fj", wantIdle: "gh"},
		{name: "falls through to the alias", github: "gh", wantFile: "gh"},
		{name: "an empty value is not set", forgejo: "", github: "gh", wantFile: "gh"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			fsys := testfs.NewReal(t)

			env := testenv.New(t)

			for name, rel := range map[string]string{
				"FORGEJO_OUTPUT": testCase.forgejo,
				"GITHUB_OUTPUT":  testCase.github,
			} {
				if rel == "" {
					env.Setenv(name, "")

					continue
				}

				env.Setenv(name, fsys.WriteFile(rel, nil))
			}

			s := ghaoutput.NewFromEnvVars("FORGEJO_OUTPUT", "GITHUB_OUTPUT")
			require.NoError(t, s.Set(context.Background(), "k", "v"))
			require.NoError(t, s.Close(context.Background()))

			if got := string(fsys.ReadFile(testCase.wantFile)); got != "k=v\n" {
				t.Errorf("%s = %q, want %q", testCase.wantFile, got, "k=v\n")
			}

			if testCase.wantIdle != "" {
				if data := fsys.ReadFile(testCase.wantIdle); len(data) != 0 {
					t.Errorf("%s was written to as well: %q", testCase.wantIdle, data)
				}
			}
		})
	}
}

// TestNewFromEnvVars_FallsBackToDevNullWhenNoNameIsSet keeps a local run
// (no runner, no output file) from failing on an output write. A fallback of
// "" instead of os.DevNull would surface here as an open error.
func TestNewFromEnvVars_FallsBackToDevNullWhenNoNameIsSet(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("FORGEJO_OUTPUT", "")
	env.Setenv("GITHUB_OUTPUT", "")

	s := ghaoutput.NewFromEnvVars("FORGEJO_OUTPUT", "GITHUB_OUTPUT")
	require.NoError(t, s.Set(context.Background(), "k", "v"))
	require.NoError(t, s.SetMultiline(context.Background(), "m", []string{"a", "b"}))
	require.NoError(t, s.Close(context.Background()))
}
