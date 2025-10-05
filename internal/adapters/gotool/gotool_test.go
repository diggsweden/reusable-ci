// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gotool_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gotool"
	domainbuild "github.com/diggsweden/reusable-ci/v3/internal/domain/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
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
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("a non-zero exit = %v, want ErrValidation", err)
	}

	// The subcommand is in the message so the operator knows which step
	// failed without reading the whole log.
	if !strings.Contains(err.Error(), "test") {
		t.Errorf("error should name the subcommand: %v", err)
	}
}

// TestGo_RunClassifiesAMissingBinaryAsUnavailable is the other half the
// policy names, and the half that had no test: a toolchain that is not
// installed is the environment being incomplete, not the build being
// wrong. Collapsing it into ErrValidation would exit 65 and read to an
// operator as "your code failed" instead of "install Go".
func TestGo_RunClassifiesAMissingBinaryAsUnavailable(t *testing.T) {
	// An empty PATH, so the lookup genuinely fails rather than depending
	// on what happens to be installed on the host.
	t.Setenv("PATH", t.TempDir())

	err := gotool.Go{}.Run(context.Background(), domainbuild.GoRunInput{
		Args:   []string{"build"},
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	})
	if !errors.Is(err, errs.ErrDependencyUnavailable) {
		t.Fatalf("missing go binary = %v, want ErrDependencyUnavailable", err)
	}

	if errors.Is(err, errs.ErrValidation) {
		t.Errorf("a missing toolchain must not also read as a build failure: %v", err)
	}
}

// TestGo_RunExtendsTheEnvironmentAndForwardsStderr: in.Env is appended to the
// inherited environment, so a variable the build did not mention (GOFLAGS,
// GOPROXY, the module cache) still reaches go; and the tool's diagnostics go
// to the caller's stderr rather than being swallowed.
func TestGo_RunExtendsTheEnvironmentAndForwardsStderr(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("go", `printf 'marker=%s goos=%s\n' "${REUSABLE_CI_TEST_MARKER:-unset}" "${GOOS:-unset}"; printf 'go: warning\n' >&2`)
	t.Setenv("REUSABLE_CI_TEST_MARKER", "inherited")

	var stdout, stderr bytes.Buffer

	err := gotool.Go{}.Run(context.Background(), domainbuild.GoRunInput{
		Args: []string{"build"}, Env: []string{"GOOS=windows"}, Stdout: &stdout, Stderr: &stderr,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := strings.TrimSpace(stdout.String()); got != "marker=inherited goos=windows" {
		t.Errorf("go saw %q; the environment was replaced rather than extended", got)
	}

	if strings.TrimSpace(stderr.String()) != "go: warning" {
		t.Errorf("stderr = %q, want the tool's diagnostics forwarded", stderr.String())
	}
}
