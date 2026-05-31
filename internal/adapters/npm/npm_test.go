// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package npm_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/npm"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

func TestNew(t *testing.T) {
	if npm.New() == nil {
		t.Fatal("New returned nil")
	}
}

func TestAdapter_RunInherit(t *testing.T) {
	m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	m.Add("npm", `printf 'npm %s\n' "$*"; printf 'warn\n' >&2`)
	a := &npm.Adapter{Bin: m.Path("npm")}

	var stdout, stderr bytes.Buffer
	if err := a.RunInherit(context.Background(), "", &stdout, &stderr, "version", "1.2.3", "--no-git-tag-version"); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(stdout.String(), "npm version 1.2.3 --no-git-tag-version") {
		t.Errorf("stdout = %q", stdout.String())
	}

	if strings.TrimSpace(stderr.String()) != "warn" {
		t.Errorf("stderr = %q", stderr.String())
	}

	if got := m.Invocations("npm")[0].Args; len(got) != 3 || got[0] != "version" || got[1] != "1.2.3" || got[2] != "--no-git-tag-version" {
		t.Errorf("args = %v", got)
	}
}

func TestAdapter_RunInheritWrapsFailure(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("npm", `exit 7`)
	a := &npm.Adapter{Bin: m.Path("npm")}

	err := a.RunInherit(context.Background(), "", &bytes.Buffer{}, &bytes.Buffer{}, "version", "1.2.3")
	if err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("err = %v", err)
	}
}

func TestAdapter_RunCapturesOutput(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("npm", `printf 'out'; printf 'err' >&2`)
	a := &npm.Adapter{Bin: m.Path("npm")}

	stdout, stderr, err := a.Run(context.Background(), "", "pack", "--json")
	if err != nil {
		t.Fatal(err)
	}

	if stdout != "out" || stderr != "err" {
		t.Errorf("stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestAdapter_RunCapturesExitStderr(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("npm", `printf 'missing' >&2; exit 1`)
	a := &npm.Adapter{Bin: m.Path("npm")}

	_, stderr, err := a.Run(context.Background(), "", "view", "pkg@1.0.0", "version")
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(stderr, "missing") {
		t.Errorf("stderr = %q", stderr)
	}
}
