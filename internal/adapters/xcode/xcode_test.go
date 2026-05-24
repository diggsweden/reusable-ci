// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package xcode_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/adapters/xcode"
	"github.com/diggsweden/reusable-ci/internal/testutil/mockbinary"
)

func TestNewConstructors(t *testing.T) {
	if xcode.NewBuild() == nil {
		t.Fatal("NewBuild returned nil")
	}

	if xcode.NewSecurity() == nil {
		t.Fatal("NewSecurity returned nil")
	}
}

func TestXcodeBuild_RunInherit(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("xcodebuild", `printf 'xcodebuild %s\n' "$*"`)
	a := &xcode.Build{Bin: m.Path("xcodebuild")}

	var stdout bytes.Buffer

	code, err := a.RunInherit(context.Background(), &stdout, &bytes.Buffer{}, "archive", "-scheme", "App")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}

	if !strings.Contains(stdout.String(), "xcodebuild archive -scheme App") {
		t.Errorf("stdout = %q", stdout.String())
	}
}

func TestXcodeBuild_RunInheritReturnsExitCode(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("xcodebuild", `exit 65`)
	a := &xcode.Build{Bin: m.Path("xcodebuild")}

	code, err := a.RunInherit(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "archive")
	if err != nil || code != 65 {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestSecurity_RunReturnsCombinedOutput(t *testing.T) {
	m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	m.Add("security", `printf 'out'; printf 'err' >&2`)
	a := &xcode.Security{Bin: m.Path("security")}

	out, err := a.Run(context.Background(), "find-identity")
	if err != nil {
		t.Fatal(err)
	}

	if out != "outerr" {
		t.Errorf("output = %q", out)
	}

	if got := m.Invocations("security")[0].Args; len(got) != 1 || got[0] != "find-identity" {
		t.Errorf("args = %v", got)
	}
}

// TestSecurity_RunRedactsPEMOutput pins the redactor on the highest-
// likelihood leak vector: `security find-identity`, `security export`,
// and friends print PEM-encoded certificate/key material on success
// AND on partial failures. The adapter must scrub PEM markers before
// returning so callers can't accidentally surface the bytes in error
// messages or step summaries.
func TestSecurity_RunRedactsPEMOutput(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("security", `cat <<EOF
some context line
-----BEGIN RSA PRIVATE KEY-----
NEVER-SHOULD-LEAK
-----END RSA PRIVATE KEY-----
EOF`)
	a := &xcode.Security{Bin: m.Path("security")}

	out, err := a.Run(context.Background(), "export", "-k", "build.keychain")
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(out, "NEVER-SHOULD-LEAK") {
		t.Errorf("redactor missed PEM private-key block; out = %q", out)
	}

	if !strings.Contains(out, "redacted") {
		t.Errorf("redaction notice missing; out = %q", out)
	}
}
