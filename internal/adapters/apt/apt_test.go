// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package apt_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/apt"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

// TestInstall_UpdatesFirstThenInstalls pins the sequence and the flags.
// Installing without updating first fails on any runner whose package
// index predates the requested version, and --no-install-recommends is
// what keeps an unrelated dependency tree out of the build image.
func TestInstall_UpdatesFirstThenInstalls(t *testing.T) {
	m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name.
	m.Add("apt-get", `printf '%s\n' "$*"`)

	var out bytes.Buffer

	a := &apt.Adapter{Bin: m.Path("apt-get")}
	if err := a.Install(context.Background(), []string{"jq", "curl"}, &out); err != nil {
		t.Fatal(err)
	}

	want := "update\ninstall -y --no-install-recommends jq curl\n"
	if got := out.String(); got != want {
		t.Errorf("invocations =\n%q\nwant\n%q", got, want)
	}
}

// TestInstall_SetsNoninteractiveFrontend covers the variable that keeps a
// package with a debconf prompt from hanging the job until it times out.
func TestInstall_SetsNoninteractiveFrontend(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("apt-get", `printf '%s=%s\n' DEBIAN_FRONTEND "${DEBIAN_FRONTEND:-unset}"`)

	var out bytes.Buffer

	a := &apt.Adapter{Bin: m.Path("apt-get")}
	if err := a.Install(context.Background(), []string{"jq"}, &out); err != nil {
		t.Fatal(err)
	}

	if got := out.String(); strings.Contains(got, "DEBIAN_FRONTEND=unset") {
		t.Errorf("DEBIAN_FRONTEND was not set for the subprocess: %q", got)
	}

	// Both invocations, not only the install.
	if got := strings.Count(out.String(), "DEBIAN_FRONTEND=noninteractive"); got != 2 {
		t.Errorf("set on %d of 2 invocations: %q", got, out.String())
	}
}

// TestInstall_UpdateFailureStopsBeforeInstalling covers the fail-fast:
// installing against a stale index would pull whatever version happens to
// be there, so a failed update must not be followed by an install.
func TestInstall_UpdateFailureStopsBeforeInstalling(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("apt-get", `
printf '%s\n' "$*"
case "$1" in
  update) exit 100 ;;
esac
`)

	var out bytes.Buffer

	a := &apt.Adapter{Bin: m.Path("apt-get")}
	if err := a.Install(context.Background(), []string{"jq"}, &out); err == nil {
		t.Fatal("a failed update must be an error")
	}

	if strings.Contains(out.String(), "install") {
		t.Errorf("installed despite a failed update: %q", out.String())
	}
}
