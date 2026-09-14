// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package secret_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/secret"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestResolve_FilePathWins(t *testing.T) {
	fsys := testfs.NewReal(t)
	path := fsys.WriteFile("token.txt", []byte("ghp_fileval\n"))
	env := testenv.New(t)
	env.Setenv("RELEASE_TOKEN", "ghp_envval")

	got, err := secret.Resolve(path, "RELEASE_TOKEN")
	if err != nil {
		t.Fatal(err)
	}

	if got != "ghp_fileval" {
		t.Errorf("Resolve = %q, want file value with trailing newline trimmed", got)
	}
}

func TestResolve_EnvFallback(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("RELEASE_TOKEN", "ghp_envval\n")

	got, err := secret.Resolve("", "RELEASE_TOKEN")
	if err != nil {
		t.Fatal(err)
	}

	if got != "ghp_envval" {
		t.Errorf("Resolve = %q, want env value with trailing newline trimmed", got)
	}
}

func TestResolve_EmptyWhenNeitherSet(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("RELEASE_TOKEN", "")

	got, err := secret.Resolve("", "RELEASE_TOKEN")
	if err != nil {
		t.Fatal(err)
	}

	if got != "" {
		t.Errorf("Resolve = %q, want empty string", got)
	}
}

func TestResolve_MissingFileSurfacesError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.txt")

	_, err := secret.Resolve(missing, "")
	// A mistyped --*-file is the operator's path mistake: EX_NOINPUT (66),
	// not the unclassified EX_SOFTWARE (70) that reads "file a bug". Resolve
	// reads through cliio.ReadFile, which owns this classification.
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Fatalf("err = %v, want errs.ErrMissingInput", err)
	}

	// The cause survives the wrap, so the operator sees "no such file" and
	// the path they actually typed.
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want it to wrap fs.ErrNotExist", err)
	}

	if !strings.Contains(err.Error(), "read secret from") {
		t.Errorf("err = %v, want it to name the operation", err)
	}
}

// TestResolve_DirectoryPathIsClassifiedToo covers the other way an operator
// points --*-file at the wrong thing. Both used to fall through unclassified.
func TestResolve_DirectoryPathIsClassifiedToo(t *testing.T) {
	_, err := secret.Resolve(t.TempDir(), "")
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Fatalf("err = %v, want errs.ErrMissingInput", err)
	}

	if !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("err = %v, want it to say the path is a directory", err)
	}
}

func TestResolve_EmptyEnvVarNameReturnsEmpty(t *testing.T) {
	got, err := secret.Resolve("", "")
	if err != nil {
		t.Fatal(err)
	}

	if got != "" {
		t.Errorf("Resolve = %q, want empty", got)
	}
}

// TestResolve_TrimsOnlyTrailingLineEndings pins the trimming rule
// exactly. It exists so a value saved with a trailing newline does not
// "poison downstream consumers", and the sharpest consumer is the output
// sink: ghaoutput and gitlaboutput both refuse a scalar containing \r or
// \n, so an untrimmed secret is not merely untidy, it fails the run.
//
// Only \n was covered. A secret file written on Windows, or fetched
// through a tool that normalises line endings, carries \r\n.
func TestResolve_TrimsOnlyTrailingLineEndings(t *testing.T) {
	for _, tc := range []struct{ name, stored, want string }{
		{name: "unix newline", stored: "tok\n", want: "tok"},
		{name: "windows newline", stored: "tok\r\n", want: "tok"},
		{name: "bare carriage return", stored: "tok\r", want: "tok"},
		{name: "several trailing newlines", stored: "tok\n\n\n", want: "tok"},
		{name: "mixed trailing", stored: "tok\r\n\r\n", want: "tok"},
		{name: "no trailing newline", stored: "tok", want: "tok"},

		// A secret is opaque: only trailing line endings come off.
		// Leading whitespace and interior content are part of the value,
		// and trimming them would silently corrupt a valid credential.
		{name: "leading space is preserved", stored: " tok\n", want: " tok"},
		{name: "trailing space is preserved", stored: "tok \n", want: "tok "},
		{name: "interior newline is preserved", stored: "line1\nline2\n", want: "line1\nline2"},
		{name: "tab is preserved", stored: "tok\t\n", want: "tok\t"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			path := fsys.WriteFile("token.txt", []byte(tc.stored))

			got, err := secret.Resolve(path, "")
			if err != nil {
				t.Fatal(err)
			}

			if got != tc.want {
				t.Errorf("from file: Resolve = %q, want %q", got, tc.want)
			}

			// The env path applies the same rule, so a secret behaves the
			// same however the workflow supplied it.
			env := testenv.New(t)
			env.Setenv("RELEASE_TOKEN", tc.stored)

			got, err = secret.Resolve("", "RELEASE_TOKEN")
			if err != nil {
				t.Fatal(err)
			}

			if got != tc.want {
				t.Errorf("from env: Resolve = %q, want %q", got, tc.want)
			}
		})
	}
}

// swapStdin points os.Stdin at f for one test and restores it.
//
// The audit item behind these tests proposed removing this process-global
// mutation with an injected reader or a child process. Neither is taken, and
// the reason is worth stating so it is not reproposed: what these tests
// exercise IS the routing from the "-" sentinel to the process's stdin, so a
// reader threaded in as a parameter would test a different function than the
// one the CLI calls, and a child process would test the same thing far more
// slowly. The mutation is safe here because no test in this package or in
// cliio declares t.Parallel(), and it is restored through t.Cleanup on every
// path including a failing one. The rule this depends on is the comment above
// each caller: a test that swaps stdin must not be parallel.
func swapStdin(t *testing.T, f *os.File) {
	t.Helper()

	orig := os.Stdin
	os.Stdin = f

	t.Cleanup(func() { os.Stdin = orig })
}

// TestResolve_StdinDash covers the "-" path, which had no test at all.
// It is how a workflow pipes a secret in without it becoming an argv
// entry or an environment variable visible to sibling processes.
//
// Not parallel: swaps os.Stdin.
func TestResolve_StdinDash(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = r.Close() })
	swapStdin(t, r)

	go func() {
		_, _ = w.WriteString("piped-secret\r\n")
		_ = w.Close()
	}()

	got, err := secret.Resolve("-", "RELEASE_TOKEN")
	if err != nil {
		t.Fatal(err)
	}

	if got != "piped-secret" {
		t.Errorf("Resolve(\"-\") = %q, want the piped value with its line ending trimmed", got)
	}
}

// TestResolve_StdinDashRefusesATerminal is the guard that keeps a release job
// from hanging forever.
//
// "-" means "the secret is being piped in". When it is not — an operator runs
// the command by hand, or a workflow forgets the pipe — a plain read blocks on
// the terminal with no output, and in CI that is a job that burns its whole
// timeout with no indication why. cliio refuses a character device instead,
// and this pins that the refusal survives the trip through Resolve: classified
// as a usage error, naming stdin, returning no secret.
//
// /dev/null is a character device on every platform this targets, which makes
// the terminal case reachable without a TTY.
//
// Not parallel: swaps os.Stdin.
func TestResolve_StdinDashRefusesATerminal(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = f.Close() })
	swapStdin(t, f)

	got, err := secret.Resolve("-", "RELEASE_TOKEN")
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage rather than a blocking read", err)
	}

	if got != "" {
		t.Errorf("a refused read returned %q", got)
	}

	if !strings.Contains(err.Error(), "stdin") {
		t.Errorf("the error does not mention stdin: %v", err)
	}
}

// TestResolve_AnEmptyFileIsAuthoritativeOverTheEnvironment pins the precedence
// rule at the case that decides whether it is a rule at all.
//
// TestResolve_FilePathWins uses a file with content, so it cannot distinguish
// "the file wins" from "the first non-empty source wins". Those differ exactly
// when an operator points --*-file at a file that turns out to be empty: under
// file-wins they get an empty secret and a clear downstream failure; under
// first-non-empty they silently get whatever the runner happened to have in the
// environment. That second value is not one the operator chose — it may be a
// different credential entirely, left over from another step — and using it is
// worse than failing.
//
// A file that exists and is empty is not exotic: it is what `: > secret.txt`,
// a failed extraction, or a secret that did not exist in the vault produces.
func TestResolve_AnEmptyFileIsAuthoritativeOverTheEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stored string
	}{
		{name: "a zero-byte file", stored: ""},
		{name: "a file holding only a newline", stored: "\n"},
		{name: "a file holding only CRLF", stored: "\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			path := fsys.WriteFile("secret.txt", []byte(tc.stored))

			env := testenv.New(t)
			env.Setenv("RELEASE_TOKEN", "ghp_fallback_value")

			got, err := secret.Resolve(path, "RELEASE_TOKEN")
			if err != nil {
				t.Fatal(err)
			}

			if got != "" {
				t.Errorf("Resolve = %q, want the empty file to win over the populated environment", got)
			}
		})
	}

	// The control: with no file named at all, the environment is used. Without
	// this, a Resolve that always returned "" would satisfy the cases above.
	env := testenv.New(t)
	env.Setenv("RELEASE_TOKEN", "ghp_fallback_value")

	got, err := secret.Resolve("", "RELEASE_TOKEN")
	if err != nil {
		t.Fatal(err)
	}

	if got != "ghp_fallback_value" {
		t.Errorf("Resolve with no file = %q, want the environment value", got)
	}
}
