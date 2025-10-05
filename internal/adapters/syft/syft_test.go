// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package syft_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/syft"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

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
	if got := m.Invocations("syft")[0].Args; !slices.Equal(got, wantArgs) {
		t.Errorf("args = %v, want %v", got, wantArgs)
	}

	if strings.TrimSpace(stderr.String()) != "ok" {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestAdapter_GenerateRequiresOutputs(t *testing.T) {
	a := &syft.Adapter{Bin: "syft"}

	err := a.Generate(context.Background(), "./target", nil, &bytes.Buffer{})
	if !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), "no outputs requested") {
		t.Fatalf("err = %v, want ErrUsage naming the missing outputs", err)
	}
}

func TestAdapter_GenerateCanUnsetSensitiveEnv(t *testing.T) {
	m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name.
	m.Add("syft", `
if [ -n "${COSIGN_KEY:-}" ] || [ -n "${REGISTRY_TOKEN:-}" ]; then
  printf 'secret env leaked to syft\n' >&2
  exit 42
fi
printf 'ok\n' >&2
`)
	t.Setenv("COSIGN_KEY", "secret")
	t.Setenv("REGISTRY_TOKEN", "token")

	a := &syft.Adapter{Bin: m.Path("syft"), UnsetEnv: []string{"COSIGN_KEY", "REGISTRY_TOKEN"}}

	var stderr bytes.Buffer
	if err := a.Generate(context.Background(), "image@sha256:abc", map[string]string{"cyclonedx-json": "out.json"}, &stderr); err != nil {
		t.Fatal(err)
	}

	if strings.TrimSpace(stderr.String()) != "ok" {
		t.Errorf("stderr = %q", stderr.String())
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

// TestSyft_MissingBinaryIsADependencyProblem covers both entry points.
//
// syft is installed by the toolchain step, so "not on PATH" means that step
// did not run or did not finish -- an environment problem the operator can
// act on. Unclassified it exited 70, "file a bug against reusable-ci".
func TestSyft_MissingBinaryIsADependencyProblem(t *testing.T) {
	adapter := &syft.Adapter{Bin: "syft-does-not-exist"}

	for name, run := range map[string]func() error{
		"Generate": func() error {
			return adapter.Generate(context.Background(), "img",
				map[string]string{"spdx-json": filepath.Join(t.TempDir(), "out.json")}, io.Discard)
		},
		"RunInherit": func() error {
			return adapter.RunInherit(context.Background(), io.Discard, io.Discard, "--version")
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := run()
			if !errors.Is(err, errs.ErrDependencyUnavailable) {
				t.Fatalf("err = %v, want ErrDependencyUnavailable", err)
			}

			if !strings.Contains(err.Error(), "syft-does-not-exist") {
				t.Errorf("err = %v, want it to name the binary", err)
			}
		})
	}
}
