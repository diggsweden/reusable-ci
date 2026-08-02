// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

const jobGraphClean = `jobs:
  p:
    if: ${{ always() }}
    outputs:
      x: ${{ steps.s.outputs.x }}
    steps: []
  c:
    needs: [p]
    if: ${{ needs.p.outputs.x != '' }}
    uses: ./.github/workflows/s.yml
`

const jobGraphMasking = `jobs:
  p:
    if: ${{ github.ref == 'refs/heads/main' }}
    outputs:
      x: ${{ steps.s.outputs.x }}
    steps: []
  c:
    needs: [p]
    if: ${{ needs.p.outputs.x != '' }}
    uses: ./.github/workflows/s.yml
`

func TestJobGraph_CleanPasses(t *testing.T) {
	mem := testfs.NewMemory(t)
	mem.WriteFile(".github/workflows/ok.yml", []byte(jobGraphClean))

	var out bytes.Buffer
	if err := appvalidate.JobGraph(&out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.JobGraphInput{Root: ".", FS: mem.FS()}); err != nil {
		t.Fatalf("JobGraph: %v", err)
	}
}

func TestJobGraph_MaskingFails(t *testing.T) {
	mem := testfs.NewMemory(t)
	mem.WriteFile(".github/workflows/bad.yml", []byte(jobGraphMasking))

	var out bytes.Buffer

	err := appvalidate.JobGraph(&out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.JobGraphInput{Root: ".", FS: mem.FS()})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if !strings.Contains(out.String(), "masked") {
		t.Errorf("missing masking annotation in:\n%s", out.String())
	}
}
