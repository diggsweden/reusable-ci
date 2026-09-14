// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package scripts

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
)

// TestPrintfFormatsCannotBeEatenAsOptions is the parsed replacement for the
// old lexical tripwire. shellsafety_test.go explains what it looks for and why
// a regex could not; this test is the walk over the repository's own scripts.
//
// The scan covers every shell source in the repository, not just this
// directory's: the walk used to start at "." and so missed the livetest
// credential probes, which handle secrets and are the last place a swallowed
// diagnostic should go unnoticed.
func TestPrintfFormatsCannotBeEatenAsOptions(t *testing.T) {
	t.Parallel()

	var findings []string

	scanned, err := walkShellSources(reporoot.Path(t), func(path string, body []byte) error {
		calls, parseErr := unsafePrintfCalls(path, body)
		if parseErr != nil {
			return parseErr
		}

		for _, call := range calls {
			findings = append(findings, fmt.Sprintf("%s:%d: printf %s (write `printf -- %s` or drop the leading dash)",
				path, call.Line, call.Text, call.Text))
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if scanned == 0 {
		t.Fatal("no shell scripts found; this guard would pass vacuously")
	}

	if len(findings) > 0 {
		t.Fatalf("printf formats that the shell would consume as options:\n%s", strings.Join(findings, "\n"))
	}
}

// TestUnsafePrintfCalls_SeesCommandsAndNotData is the fixture half. Each case
// names the shape and whether the old regex got it right, because that is the
// evidence that replacing it was worth doing rather than a lateral move.
func TestUnsafePrintfCalls_SeesCommandsAndNotData(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		script string
		want   bool
	}{
		{"double-quoted dash format", `printf "-n%s" "$x"`, true},
		{"single-quoted dash format (regex missed)", `printf '-n%s' "$x"`, true},
		{"unquoted dash format (regex missed)", `printf -n%s "$x"`, true},
		{"double-dash format (regex missed)", `printf "--verbose=%s" "$x"`, true},
		{"safe printf first, unsafe second on one line (regex missed)", `printf 'ok\n'; printf '-n'`, true},
		{"inside a command substitution (regex missed)", "x=\"$(printf '-n')\"", true},
		{"inside a function body", "f() {\n  printf '-n'\n}", true},
		{"inside an if branch", "if [ -n \"$x\" ]; then printf '-n'; fi", true},

		{"end-of-options marker", `printf -- "-n%s" "$x"`, false},
		{"ordinary format", `printf '%s\n' "$x"`, false},
		{"format that merely contains a dash", `printf 'a-b\n'`, false},
		{"comment mentioning the unsafe form (regex flagged it)", `# printf "-n" would be eaten`, false},
		{"here-doc body mentioning it (regex flagged it)", "cat <<'EOF'\nprintf \"-n\"\nEOF", false},
		{"a string argument to another command", `echo 'printf "-n"'`, false},
		{"printf as a variable value, not a call", `cmd='printf "-n"'`, false},
		{"a different command taking a dash argument", `grep -n pattern file`, false},
		{"dash text as a data argument, not the format", `printf '%s\n' '-value'`, false},
		{"variable format, not statically decidable", `printf "$fmt" "$x"`, false},
		{"printf -v assigns instead of printing", `printf -v out '%s' "$x"`, false},
		{"printf -v with a dash-led format is still unsafe", `printf -v out '-n' "$x"`, true},
		{"-- after an option", `printf -v out -- '-n'`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			calls, err := unsafePrintfCalls("fixture.sh", []byte(tc.script+"\n"))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}

			if got := len(calls) > 0; got != tc.want {
				t.Errorf("found %v, want %v for:\n%s", calls, tc.want, tc.script)
			}
		})
	}
}

// The finding carries the line the format sits on, so a long script points at
// the call rather than at the file. A multi-line command is the case that makes
// this worth asserting: the line of the format word is not the line the call
// started on.
func TestUnsafePrintfCalls_ReportTheFormatsOwnLine(t *testing.T) {
	t.Parallel()

	script := "printf -- '-safe\\n'\n" +
		"printf '%s\\n' ok\n" +
		"printf \\\n  '-unsafe'\n"

	calls, err := unsafePrintfCalls("fixture.sh", []byte(script))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(calls) != 1 || calls[0].Line != 4 || calls[0].Text != "-unsafe" {
		t.Errorf("got %v, want one finding at line 4 for %q", calls, "-unsafe")
	}
}

// The probe templates are the reason a parser needs help: a marker at the start
// of a line stands for statements, the same marker elsewhere stands for a
// value, and neither is shell. These cases pin that reading, including that the
// substitution does not quietly hide a printf that is really in the template.
func TestUnsafePrintfCalls_ParsesProbeTemplates(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		script string
		want   bool
	}{
		{
			name:   "statement marker glued to the next command",
			script: "__TOKEN_FETCH__if [ -z \"$x\" ]; then\n  printf 'ok\\n'\nfi",
		},
		{
			name:   "two adjacent statement markers",
			script: "__CREDENTIAL_SETUP____PROBE_PRELUDE__\nprintf 'ok\\n'",
		},
		{
			name:   "value marker inside a quoted word",
			script: "curl -fsSL -o proxy \"__PROXY_ASSET_URL__\"",
		},
		{
			name:   "an unsafe printf in the template itself is still found",
			script: "__TOKEN_FETCH__if [ -z \"$x\" ]; then\n  printf '-n'\nfi",
			want:   true,
		},
		{
			name:   "an indented statement marker",
			script: "if true; then\n  __TOKEN_FETCH__printf 'ok\\n'\nfi",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			calls, err := unsafePrintfCalls("probe.sh.tmpl", []byte(tc.script+"\n"))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}

			if got := len(calls) > 0; got != tc.want {
				t.Errorf("found %v, want %v for:\n%s", calls, tc.want, tc.script)
			}
		})
	}

	// The same source without the template extension must fail to parse, or the
	// cases above would prove nothing about the substitution.
	if _, err := unsafePrintfCalls("probe.sh", []byte("__TOKEN_FETCH__if [ -z \"$x\" ]; then\n  printf 'ok\\n'\nfi\n")); err == nil {
		t.Error("the template substitution, not bash, is what makes these parse")
	}
}

// A script this guard cannot parse is a script it cannot vouch for. Returning
// no findings for it would be the same fail-open the regex had.
func TestUnsafePrintfCalls_RefusesUnparsableScripts(t *testing.T) {
	t.Parallel()

	if _, err := unsafePrintfCalls("broken.sh", []byte("if [ -n \"$x\" ]; then\n")); err == nil {
		t.Fatal("an unparsable script must be an error, not a clean pass")
	}
}

// shellShebang matches a sh or bash interpreter line.
var shellShebang = regexp.MustCompile(`^#!\s*(?:/bin/(?:ba)?sh|/usr/bin/env\s+(?:ba)?sh)(?:\s|$)`)

// maxShebangLine bounds how much of an extensionless file is read to classify
// it. A real interpreter line is far shorter.
const maxShebangLine = 256

// hasShellShebang reads at most the first line of path and reports whether it
// names sh or bash. The descriptor is closed before returning, so a walk over a
// large tree holds at most one open file at a time.
func hasShellShebang(root *os.Root, path string) (bool, error) {
	file, err := root.Open(path)
	if err != nil {
		return false, err
	}

	defer func() { _ = file.Close() }()

	prefix := make([]byte, maxShebangLine)

	n, err := io.ReadFull(file, prefix)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return false, err
	}

	first, _, _ := bytes.Cut(prefix[:n], []byte("\n"))

	return shellShebang.Match(first), nil
}

// Approved shell inputs: .sh, .bash and .sh.tmpl, plus extensionless files
// with a sh/bash shebang. Embedded workflow/Go/just recipes are separate scopes.
func walkShellSources(root string, visit func(string, []byte) error) (int, error) {
	handle, err := pathsafe.OpenRoot(root)
	if err != nil {
		return 0, err
	}
	defer func() { _ = handle.Close() }()

	count := 0
	err = fs.WalkDir(handle.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("linked source cannot be scanned: %s: %w", path, errs.ErrValidation)
		}

		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "dist", "target":
				return fs.SkipDir
			}

			return nil
		}

		name := entry.Name()

		approved := strings.HasSuffix(name, ".sh") || strings.HasSuffix(name, ".bash") || strings.HasSuffix(name, ".sh.tmpl")
		if !approved && filepath.Ext(name) != "" {
			return nil
		}

		// An extensionless file is only a candidate until its first line says
		// otherwise, so only that line is read. The whole body used to be read
		// first, which made every binary in the tree -- a local
		// `go build -o reusable-ci` among them -- a full read on every run,
		// and anything over the 64 MiB input bound failed the guard outright.
		if !approved {
			shell, shebangErr := hasShellShebang(handle, path)
			if shebangErr != nil || !shell {
				return shebangErr
			}
		}

		body, err := reporoot.ReadFileAt(root, filepath.FromSlash(path))
		if err != nil {
			return err
		}

		count++

		return visit(path, body)
	})

	return count, err
}

// TestWalkShellSources_ClassifiesExtensionlessFilesByTheirFirstLine drives the
// walk over an owned tree holding what a working checkout really contains: a
// .sh script, an extensionless script with a shebang, and an extensionless
// binary larger than the input bound -- the shape of a local
// `go build -o reusable-ci`. The binary used to be read whole to look at its
// first line, and at this size that read failed the guard for everyone who had
// built the tool in place.
//
// The large file is sparse, so the fixture costs no disk.
func TestWalkShellSources_ClassifiesExtensionlessFilesByTheirFirstLine(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	for name, body := range map[string]string{
		"install.sh":       "#!/bin/sh\necho ok\n",
		"bootstrap":        "#!/usr/bin/env bash\necho ok\n",
		"NOTICE":           "not a script\n",
		"sub/also-a-shell": "#!/bin/bash\necho ok\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	binary, err := os.Create(filepath.Join(root, "reusable-ci"))
	if err != nil {
		t.Fatal(err)
	}

	if truncErr := binary.Truncate(70 << 20); truncErr != nil {
		t.Fatal(truncErr)
	}

	if closeErr := binary.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	var visited []string

	scanned, err := walkShellSources(root, func(path string, _ []byte) error {
		visited = append(visited, path)

		return nil
	})
	if err != nil {
		t.Fatalf("walk failed on an ordinary checkout containing a built binary: %v", err)
	}

	// Lexical walk order, so the list is deterministic.
	if want := []string{"bootstrap", "install.sh", "sub/also-a-shell"}; scanned != len(want) || !slices.Equal(visited, want) {
		t.Errorf("visited %v (scanned %d), want %v", visited, scanned, want)
	}
}
