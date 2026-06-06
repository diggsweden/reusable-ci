// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package opengrep_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/adapters/opengrep"
	"github.com/diggsweden/reusable-ci/internal/testutil/mockbinary"
)

func TestNew(t *testing.T) {
	if opengrep.New() == nil {
		t.Fatal("New returned nil")
	}
}

func TestAdapter_RunInheritPassesArgsAndEnv(t *testing.T) {
	m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	m.Add("opengrep", `printf '%s\n' "$PYTHONWARNINGS"`)
	a := &opengrep.Adapter{Bin: m.Path("opengrep")}

	var stdout bytes.Buffer

	code, err := a.RunInherit(context.Background(), &stdout, &bytes.Buffer{}, "scan", "--config", "p/default")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}

	if strings.TrimSpace(stdout.String()) != "ignore:RequestsDependencyWarning" {
		t.Errorf("stdout = %q", stdout.String())
	}

	if got := m.Invocations("opengrep")[0].Args; len(got) != 3 || got[0] != "scan" || got[2] != "p/default" {
		t.Errorf("args = %v", got)
	}
}

func TestAdapter_RunInheritReturnsExitCode(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("opengrep", `exit 3`)
	a := &opengrep.Adapter{Bin: m.Path("opengrep")}

	code, err := a.RunInherit(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "scan")
	if err != nil || code != 3 {
		t.Fatalf("code=%d err=%v", code, err)
	}
}
