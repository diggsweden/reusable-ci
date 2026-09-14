// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fakeGitBinary is a stand-in `git` executable owned by this package.
//
// The argv-recording tests in this file use mockbinary, which is the right
// tool for "what was git asked to do": it records argv, env, stdin and cwd
// for any number of stubs. It builds those recordings with jq, which is a
// host dependency the suite declares but does not install.
//
// The tests below need something different and much smaller. They are about
// how the adapter CLASSIFIES a failing git — which stderr text maps to which
// sentinel — so what they need is a process that exits non-zero with a chosen
// message, plus a way to see the environment it was given. Recording argv as
// JSON is not part of that, and neither is jq.
//
// So this is a plain POSIX-sh script writing plain lines: no bashisms, no jq,
// nothing to install. It is deliberately not general. Anything that needs to
// assert a whole argv sequence should keep using mockbinary rather than grow
// this into a second framework.
type fakeGitBinary struct {
	path    string
	envFile string
}

// newFakeGitBinary writes a `git` that prints stdout, prints stderr, records
// the locale variables it was run with, and exits with exitCode.
//
// Tests using it must not call t.Parallel(). Writing a file and then executing
// it races the runtime: a fork on another thread inherits the still-open write
// descriptor and the exec fails with ETXTBSY. That is a property of writing
// executables at test time, not of this helper, and the fix is to keep these
// few tests serial rather than to retry around it.
func newFakeGitBinary(t *testing.T, stdout, stderr string, exitCode int) *fakeGitBinary {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "git")
	envFile := filepath.Join(dir, "env.txt")

	// Single-quoted sh strings: the only character needing care is the quote
	// itself, and no fixture here contains one.
	for _, s := range []string{stdout, stderr, envFile} {
		if strings.Contains(s, "'") {
			t.Fatalf("fakeGitBinary fixture contains a single quote, which this stub does not escape: %q", s)
		}
	}

	script := "#!/bin/sh\n" +
		"printf 'LC_ALL=%s\\nLANG=%s\\nLC_MESSAGES=%s\\nRCI_TEST_PROBE=%s\\n' " +
		"\"$LC_ALL\" \"$LANG\" \"$LC_MESSAGES\" \"$RCI_TEST_PROBE\" > '" + envFile + "'\n" +
		"printf '%s' '" + stdout + "'\n" +
		"printf '%s' '" + stderr + "' >&2\n" +
		"exit " + strconv.Itoa(exitCode) + "\n"

	if err := os.WriteFile(path, []byte(script), 0o755); err != nil { //nolint:gosec // test stub must be executable.
		t.Fatalf("write fake git: %v", err)
	}

	return &fakeGitBinary{path: path, envFile: envFile}
}

// env returns the environment variables the stub observed, as "NAME=value"
// lines. RCI_TEST_PROBE is carried so a test can prove an ordinary inherited
// variable survived, not only the ones the adapter sets itself.
func (f *fakeGitBinary) env(t *testing.T) string {
	t.Helper()

	body, err := os.ReadFile(f.envFile)
	if err != nil {
		t.Fatalf("the fake git was never run: %v", err)
	}

	return string(body)
}
