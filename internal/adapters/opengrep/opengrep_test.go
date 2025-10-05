// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package opengrep_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/opengrep"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

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

	// Every slot, including the "--config" flag itself: checking only the
	// ends left the middle argument free to be anything.
	want := []string{"scan", "--config", "p/default"}
	if got := m.Invocations("opengrep")[0].Args; !slices.Equal(got, want) {
		t.Errorf("args = %v, want %v", got, want)
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

// TestAdapter_RunInheritReportsAMissingBinaryAsUnavailable: a scan whose
// binary is absent must not look like a clean scan. Exit code 0 with a nil
// error is exactly what a caller reads as "no findings", so the missing
// tool is a classified error and a non-zero code.
func TestAdapter_RunInheritReportsAMissingBinaryAsUnavailable(t *testing.T) {
	t.Parallel()

	a := &opengrep.Adapter{Bin: filepath.Join(t.TempDir(), "opengrep")}

	code, err := a.RunInherit(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "scan")
	if !errors.Is(err, errs.ErrDependencyUnavailable) || code == 0 {
		t.Fatalf("code=%d err=%v, want ErrDependencyUnavailable and a non-zero code", code, err)
	}
}
