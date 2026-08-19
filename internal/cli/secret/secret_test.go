// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package secret_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/secret"
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
	if err == nil || !strings.Contains(err.Error(), "read secret from") {
		t.Errorf("err = %v, want wrapped read-secret error", err)
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

// TestResolve_StdinDash covers the "-" path, which had no test at all.
// It is how a workflow pipes a secret in without it becoming an argv
// entry or an environment variable visible to sibling processes.
func TestResolve_StdinDash(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	orig := os.Stdin
	os.Stdin = r

	t.Cleanup(func() { os.Stdin = orig })

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

	_ = r.Close()
}
