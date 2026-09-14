// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cargo_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cargo"
	domainbuild "github.com/diggsweden/reusable-ci/v3/internal/domain/build"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

func TestAdapter_RunInheritAndAvailable(t *testing.T) {
	m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	m.Add("cargo", `printf 'cargo %s\n' "$*"; printf 'warn\n' >&2`)
	a := &cargo.Adapter{Bin: m.Path("cargo")} //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	if !a.Available() {
		t.Fatal("expected mocked cargo to be available")
	}

	var stdout, stderr bytes.Buffer
	if err := a.RunInherit(context.Background(), "", &stdout, &stderr, "update", "--workspace"); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(stdout.String(), "cargo update --workspace") {
		t.Errorf("stdout = %q", stdout.String())
	}

	if strings.TrimSpace(stderr.String()) != "warn" {
		t.Errorf("stderr = %q", stderr.String())
	}

	if got := m.Invocations("cargo")[0].Args; len(got) != 2 || got[0] != "update" || got[1] != "--workspace" {
		t.Errorf("args = %v", got)
	}
}

func TestAdapter_AvailableFalseForMissingBinary(t *testing.T) {
	m := mockbinary.New(t)

	a := &cargo.Adapter{Bin: m.Path("missing")}
	if a.Available() {
		t.Fatal("missing binary should not be available")
	}
}

// TestAdapter_RunForwardsEnvOnTopOfTheEnvironment covers the cross-compile
// contract: the linker override reaches cargo, and it is added to the
// inherited environment rather than replacing it, so cargo still finds its
// home, its PATH and the runner's credentials. The working directory and
// the output streams travel with the request.
func TestAdapter_RunForwardsEnvOnTopOfTheEnvironment(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("cargo", `printf 'linker=%s marker=%s cwd=%s\n' "${CARGO_TARGET_AARCH64_UNKNOWN_LINUX_GNU_LINKER:-unset}" "${REUSABLE_CI_TEST_MARKER:-unset}" "$(pwd)"; printf 'warn\n' >&2`)
	t.Setenv("REUSABLE_CI_TEST_MARKER", "inherited")

	dir := t.TempDir()

	var stdout, stderr bytes.Buffer

	err := (&cargo.Adapter{Bin: m.Path("cargo")}).Run(context.Background(), domainbuild.GoRunInput{
		Args: []string{"build", "--release"}, Dir: dir,
		Env:    []string{"CARGO_TARGET_AARCH64_UNKNOWN_LINUX_GNU_LINKER=aarch64-linux-gnu-gcc"},
		Stdout: &stdout, Stderr: &stderr,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := strings.TrimSpace(stdout.String()), "linker=aarch64-linux-gnu-gcc marker=inherited cwd="+dir; got != want {
		t.Errorf("cargo saw %q, want %q", got, want)
	}

	if strings.TrimSpace(stderr.String()) != "warn" {
		t.Errorf("stderr = %q, want cargo's own diagnostics", stderr.String())
	}
}

// TestAdapter_VersionIsTrimmed: the version is compared and rendered as one
// token, so cargo's trailing newline is not part of it.
func TestAdapter_VersionIsTrimmed(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("cargo", `printf 'cargo 1.80.0 (f6e511eec 2024-07-21)\n'`)

	got, err := (&cargo.Adapter{Bin: m.Path("cargo")}).Version(context.Background())
	if err != nil || got != "cargo 1.80.0 (f6e511eec 2024-07-21)" {
		t.Fatalf("Version = %q, %v", got, err)
	}
}
