// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/internal/app/validate"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestContractResidue_Success(t *testing.T) {
	mem := testfs.NewMemory(t)
	mem.WriteFile(".github/workflows/build.yml", []byte("value: ${{ steps.meta.outputs.version }}\n"))
	mem.WriteFile("internal/app/example.go", []byte("sink.Set(ctx, \"version\", v)\n"))

	var out bytes.Buffer
	if err := appvalidate.ContractResidue(&out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.ContractResidueInput{Root: ".", FS: mem.FS()}); err != nil {
		t.Fatalf("ContractResidue: %v", err)
	}

	if got := out.String(); got != "V3 contracts look valid.\n" {
		t.Errorf("out = %q", got)
	}
}

func TestContractResidue_ReportsRemovedContracts(t *testing.T) {
	mem := testfs.NewMemory(t)
	mem.WriteFile(".github/workflows/release.yml", []byte(strings.Join([]string{
		"outputs:",
		"  old: ${{ steps.parse.outputs.maven-artifacts }}",
		"  metadata: ${{ steps.meta.outputs.VERSION }}",
		"  npm: ${{ steps.check.outputs.already_published }}",
	}, "\n")+"\n"))

	var out bytes.Buffer

	err := appvalidate.ContractResidue(&out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.ContractResidueInput{Root: ".", FS: mem.FS()})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	body := out.String()
	for _, want := range []string{
		"legacy parse-artifacts output",
		"legacy uppercase metadata output",
		"legacy underscored output alias",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}
