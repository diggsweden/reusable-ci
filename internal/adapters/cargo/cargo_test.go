// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cargo_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cargo"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

func TestNew(t *testing.T) {
	if cargo.New() == nil {
		t.Fatal("New returned nil")
	}
}

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
