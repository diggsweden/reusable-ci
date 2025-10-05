// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package mockbinary_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

// This file pins what a recording is allowed to lose, which is nothing.
//
// mockbinary is how 23 packages assert the argv their adapters build, so a
// recording that quietly differs from the real invocation makes all of those
// assertions describe something that did not happen. The previous encoder
// built JSON with jq and lost three things without ever failing: an argument
// containing a newline arrived as two arguments, a call with no arguments
// arrived as one empty argument, and every cwd carried a trailing newline
// from `pwd | jq -Rs .`. Piped stdin lost its trailing newlines separately,
// to command substitution.
//
// Each of those is a plausible-looking recording, which is why none of them
// was noticed. They are all reachable from real fixtures: git tag and commit
// messages are multi-line, "was this tool called with no arguments" is an
// ordinary assertion, and a signing payload piped to a tool ends in a newline.

// runStub invokes a stub directly and returns its single recorded invocation.
func runStub(t *testing.T, stdin string, args ...string) mockbinary.Invocation {
	t.Helper()

	m := mockbinary.New(t)
	m.Add("probe", "exit 0")

	//nolint:gosec // G204: the binary is this helper's own stub under t.TempDir(); the arguments are the fixture under test.
	cmd := exec.CommandContext(context.Background(), m.Path("probe"), args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	if err := cmd.Run(); err != nil {
		t.Fatalf("run stub: %v", err)
	}

	got := m.Invocations("probe")
	if len(got) != 1 {
		t.Fatalf("recorded %d invocations, want 1", len(got))
	}

	return got[0]
}

func TestMockbinary_RecordsArgvExactly(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		why  string
	}{
		{
			name: "an argument containing a newline stays one argument",
			args: []string{"-m", "Release v1.0.0\n\nSigned-off-by: someone"},
			why:  "git tag -m and git commit -m carry multi-line messages",
		},
		{name: "an empty argument is preserved", args: []string{"--name", "", "--other"}},
		{name: "spaces and tabs do not split", args: []string{"a b", "c\td"}},
		{name: "quotes and backslashes survive", args: []string{`"quoted"`, `back\slash`, `$notavar`}},
		{name: "a single argument", args: []string{"status"}},
		{
			name: "no arguments at all",
			args: nil,
			why:  "must be zero arguments, not one empty one, or len(Args) lies",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runStub(t, "", tc.args...)

			want := tc.args
			if want == nil {
				want = []string{}
			}

			if !slices.Equal(got.Args, want) {
				t.Errorf("recorded %q, want %q: %s", got.Args, want, tc.why)
			}
		})
	}
}

func TestMockbinary_RecordsStdinExactly(t *testing.T) {
	for _, tc := range []struct {
		name, stdin, why string
	}{
		{
			name:  "a trailing newline is kept",
			stdin: "payload\n",
			why:   "command substitution strips them; a tool that is piped one receives it",
		},
		{name: "several trailing newlines are all kept", stdin: "payload\n\n\n"},
		{name: "no trailing newline", stdin: "payload"},
		{name: "interior newlines", stdin: "first\nsecond\n"},
		{name: "leading whitespace", stdin: "  indented\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runStub(t, tc.stdin).Stdin; got != tc.stdin {
				t.Errorf("recorded stdin %q, want %q: %s", got, tc.stdin, tc.why)
			}
		})
	}
}

// TestMockbinary_RecordsTheDirectoryItRanIn pins the cwd field, which used to
// arrive with a trailing newline — so comparing it to a directory path never
// matched and any test doing so had to know to trim.
func TestMockbinary_RecordsTheDirectoryItRanIn(t *testing.T) {
	dir := t.TempDir()

	m := mockbinary.New(t)
	m.Add("probe", "exit 0")

	//nolint:gosec // G204: the binary is this helper's own stub under t.TempDir().
	cmd := exec.CommandContext(context.Background(), m.Path("probe"))
	cmd.Dir = dir

	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	got := m.Invocations("probe")[0].Cwd
	if strings.HasSuffix(got, "\n") {
		t.Errorf("cwd %q ends with a newline the shell added", got)
	}

	// t.TempDir can hand back a symlinked path (/var vs /private/var on
	// macOS), so compare what both sides resolve to.
	wantResolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}

	gotResolved, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("recorded cwd %q is not a usable path: %v", got, err)
	}

	if gotResolved != wantResolved {
		t.Errorf("cwd = %q, want %q", gotResolved, wantResolved)
	}
}

// Concurrent stubs are not covered here, and the reason is worth stating.
//
// They interleave: bash's printf writes once per conversion, so a record
// reaches the file as one write per field, and 40 concurrent invocations
// produced a file that parsed into 11 records and then a torn one. An earlier
// draft of this file asserted the opposite — that a single printf is a single
// atomic write — and that was simply wrong.
//
// Making it atomic in shell needs a lock, or a framing that can be assembled
// in a variable before the write, and a variable cannot hold the NUL this
// framing is built on. That is machinery for a case nothing in this repository
// has: callers invoke tools in sequence.
//
// What has to hold instead is that a torn file is REFUSED rather than parsed
// into invocations nobody made — a test reading fabricated argv passes while
// describing something that never happened, which is worse than a loud
// failure. The truncated and overrunning-argc cases in
// recordings_internal_test.go are that guarantee.
