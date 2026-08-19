// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gotool_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gotool"
	domainbuild "github.com/diggsweden/reusable-ci/v3/internal/domain/build"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

// TestGo_RunPassesArgsDirAndEnv covers the three things run() threads
// through to the subprocess.
func TestGo_RunPassesArgsDirAndEnv(t *testing.T) {
	m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name.
	m.Add("go", `printf 'args=%s cwd=%s goos=%s\n' "$*" "$(pwd)" "${GOOS:-unset}"`)

	dir := t.TempDir()

	var stdout bytes.Buffer

	err := gotool.Go{}.Run(context.Background(), domainbuild.GoRunInput{
		Args:   []string{"build", "./..."},
		Dir:    dir,
		Env:    []string{"GOOS=windows"},
		Stdout: &stdout,
		Stderr: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := stdout.String()
	if !strings.Contains(got, "args=build ./...") {
		t.Errorf("args not passed through: %q", got)
	}

	if !strings.Contains(got, "goos=windows") {
		t.Errorf("Env not applied: %q", got)
	}

	// The working directory decides which module is operated on.
	if !strings.Contains(got, "cwd="+dir) {
		t.Errorf("Dir not applied (want %s): %q", dir, got)
	}
}

// TestGo_RunEnvOverridesTheAmbientValue is the property cross-compilation
// rests on. in.Env is appended to os.Environ(), and exec resolves a
// duplicate key to its last occurrence -- so a GOOS already exported by
// the runner must lose to the one the build asked for. Prepending
// instead would silently produce host binaries for every target.
func TestGo_RunEnvOverridesTheAmbientValue(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("go", `printf 'goos=%s goarch=%s cgo=%s\n' "${GOOS:-unset}" "${GOARCH:-unset}" "${CGO_ENABLED:-unset}"`)

	// The runner's environment already disagrees with the build request.
	t.Setenv("GOOS", "linux")
	t.Setenv("GOARCH", "amd64")
	t.Setenv("CGO_ENABLED", "1")

	var stdout bytes.Buffer

	err := gotool.Go{}.Run(context.Background(), domainbuild.GoRunInput{
		Args:   []string{"build"},
		Env:    []string{"CGO_ENABLED=0", "GOOS=darwin", "GOARCH=arm64"},
		Stdout: &stdout,
		Stderr: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := strings.TrimSpace(stdout.String()), "goos=darwin goarch=arm64 cgo=0"; got != want {
		t.Errorf("subprocess saw %q, want %q", got, want)
	}
}

// TestGo_RunClassifiesFailures covers the error policy the doc comment
// states: a non-zero exit and a missing binary are different conditions
// and must not collapse into one.
func TestGo_RunClassifiesFailures(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("go", `printf 'boom\n' >&2; exit 1`)

	err := gotool.Go{}.Run(context.Background(), domainbuild.GoRunInput{
		Args:   []string{"test"},
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("a non-zero exit must be an error")
	}

	// The subcommand is in the message so the operator knows which step
	// failed without reading the whole log.
	if !strings.Contains(err.Error(), "test") {
		t.Errorf("error should name the subcommand: %v", err)
	}
}
