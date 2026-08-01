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

func TestWorkflowInputDefaults_Success(t *testing.T) {
	mem := testfs.NewMemory(t)
	mem.WriteFile(".github/workflows/ok.yml", []byte("name: test\non:\n  workflow_call:\n    inputs:\n      foo:\n        default: literal\n"))
	mem.WriteFile(".github/workflows/nested/ignored.yml", []byte("      default: ${{ secrets.BAD }}\n"))

	var out bytes.Buffer
	if err := appvalidate.WorkflowInputDefaults(&out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.WorkflowInputDefaultsInput{Root: ".", FS: mem.FS()}); err != nil {
		t.Fatalf("WorkflowInputDefaults: %v", err)
	}

	if got := out.String(); got != "Workflow input defaults look valid.\n" {
		t.Errorf("out = %q", got)
	}
}

func TestWorkflowInputDefaults_ReportsExpressions(t *testing.T) {
	mem := testfs.NewMemory(t)
	body := strings.Join([]string{
		"name: bad",
		"on:",
		"  workflow_call:",
		"    inputs:",
		"      foo:",
		"        default: ${{ github.ref_name }}",
	}, "\n") + "\n"
	mem.WriteFile(".github/workflows/bad.yml", []byte(body))

	var out bytes.Buffer

	err := appvalidate.WorkflowInputDefaults(&out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.WorkflowInputDefaultsInput{Root: ".", FS: mem.FS()})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	text := out.String()
	if !strings.Contains(text, "::error file=.github/workflows/bad.yml,line=6::workflow_call input defaults must be literal values") {
		t.Errorf("missing annotation in:\n%s", text)
	}

	if !strings.Contains(text, "default: ${{ github.ref_name }}") {
		t.Errorf("missing offending line in:\n%s", text)
	}
}
