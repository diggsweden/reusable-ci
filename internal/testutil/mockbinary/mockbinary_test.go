// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package mockbinary_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

func TestMock_StubReturnsCannedOutput(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("gh", `printf 'canned-output\n'`)

	out, err := exec.CommandContext(t.Context(), m.Path("gh"), "anything").Output() //nolint:gosec // mockbinary stub path is test-generated.
	if err != nil {
		t.Fatalf("exec gh: %v", err)
	}

	if got, want := strings.TrimSpace(string(out)), "canned-output"; got != want {
		t.Errorf("gh stdout = %q, want %q", got, want)
	}
}

func TestMock_RecordsInvocationsAndArgs(t *testing.T) {
	m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	m.Add("trivy", `printf '{}\n'`)

	for i := range 3 {
		if err := exec.CommandContext(t.Context(), "trivy", "image", "--severity", "HIGH", "alpine").Run(); err != nil {
			t.Fatalf("invocation %d: %v", i, err)
		}
	}

	invs := m.Invocations("trivy")
	if len(invs) != 3 {
		t.Fatalf("expected 3 invocations, got %d", len(invs))
	}

	want := []string{"image", "--severity", "HIGH", "alpine"}
	if !equal(invs[0].Args, want) {
		t.Errorf("args = %v, want %v", invs[0].Args, want)
	}
}

func TestMock_RecordsStdin(t *testing.T) {
	m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	m.Add("gpg-connect-agent", `cat >/dev/null`)

	cmd := exec.CommandContext(t.Context(), "gpg-connect-agent", "/bye")

	cmd.Stdin = strings.NewReader("PRESET_PASSPHRASE abc123 -1 deadbeef\n")
	if err := cmd.Run(); err != nil {
		t.Fatalf("exec: %v", err)
	}

	invs := m.Invocations("gpg-connect-agent")
	if len(invs) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(invs))
	}

	if !strings.Contains(invs[0].Stdin, "PRESET_PASSPHRASE abc123") {
		t.Errorf("stdin = %q, want substring %q", invs[0].Stdin, "PRESET_PASSPHRASE abc123")
	}
}
func TestMock_AllNames(t *testing.T) {
	m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	m.Add("gh", `printf 'a\n'`)
	m.Add("trivy", `printf 'b\n'`)

	_ = exec.CommandContext(t.Context(), "gh").Run()
	_ = exec.CommandContext(t.Context(), "trivy").Run()

	names := m.AllNames()
	if !equal(names, []string{"gh", "trivy"}) {
		t.Errorf("AllNames = %v, want [gh trivy]", names)
	}
}

func equal(a, b []string) bool { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
