// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package trivy_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/adapters/trivy"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/testutil/mockbinary"
)

func TestNew(t *testing.T) {
	if trivy.New() == nil {
		t.Fatal("New returned nil")
	}
}

func TestAdapter_RunInheritPassesArgs(t *testing.T) {
	m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	m.Add("trivy", `printf 'trivy %s\n' "$*"`)
	a := &trivy.Adapter{Bin: m.Path("trivy")}

	var stdout bytes.Buffer

	code, err := a.RunInherit(context.Background(), &stdout, &bytes.Buffer{}, "image", "alpine")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}

	if !strings.Contains(stdout.String(), "trivy image alpine") {
		t.Errorf("stdout = %q", stdout.String())
	}

	if got := m.Invocations("trivy")[0].Args; len(got) != 2 || got[0] != "image" || got[1] != "alpine" {
		t.Errorf("args = %v", got)
	}
}

func TestAdapter_RunInheritReturnsExitCode(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("trivy", `exit 5`)
	a := &trivy.Adapter{Bin: m.Path("trivy")}

	code, err := a.RunInherit(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "image", "alpine")
	if err != nil || code != 5 {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestAdapter_RunInheritMissingBinaryClassifies(t *testing.T) {
	// A non-exit failure (trivy absent from PATH) must classify as
	// ErrDependencyUnavailable (EX_UNAVAILABLE 69), not fall through to
	// the unclassified internal-bug default.
	a := &trivy.Adapter{Bin: "trivy-does-not-exist-xyz"}

	_, err := a.RunInherit(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "image", "alpine")
	if !errors.Is(err, errs.ErrDependencyUnavailable) {
		t.Errorf("err = %v, want wrapped errs.ErrDependencyUnavailable", err)
	}
}
