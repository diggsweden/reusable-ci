// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package syft_test

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/adapters/syft"
	"github.com/diggsweden/reusable-ci/internal/testutil/mockbinary"
)

func TestNew(t *testing.T) {
	if syft.New() == nil {
		t.Fatal("New returned nil")
	}
}

func TestAdapter_GenerateUsesSortedOutputArgs(t *testing.T) {
	m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	m.Add("syft", `printf 'ok\n' >&2`)
	a := &syft.Adapter{Bin: m.Path("syft")}

	var stderr bytes.Buffer

	err := a.Generate(context.Background(), "./target", map[string]string{
		"spdx-json":      "out.spdx.json",
		"cyclonedx-json": "out.cyclonedx.json",
	}, &stderr)
	if err != nil {
		t.Fatal(err)
	}

	wantArgs := []string{"./target", "-o", "cyclonedx-json=out.cyclonedx.json", "-o", "spdx-json=out.spdx.json"}
	if got := m.Invocations("syft")[0].Args; !reflect.DeepEqual(got, wantArgs) {
		t.Errorf("args = %v, want %v", got, wantArgs)
	}

	if strings.TrimSpace(stderr.String()) != "ok" {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestAdapter_GenerateRequiresOutputs(t *testing.T) {
	a := &syft.Adapter{Bin: "syft"}

	err := a.Generate(context.Background(), "./target", nil, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "no outputs requested") {
		t.Fatalf("err = %v", err)
	}
}

func TestAdapter_RunInherit(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("syft", `printf 'syft %s\n' "$*"`)
	a := &syft.Adapter{Bin: m.Path("syft")}

	var stdout bytes.Buffer
	if err := a.RunInherit(context.Background(), &stdout, &bytes.Buffer{}, "--version"); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(stdout.String(), "syft --version") {
		t.Errorf("stdout = %q", stdout.String())
	}
}
