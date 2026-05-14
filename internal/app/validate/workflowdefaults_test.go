// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/internal/app/validate"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestWorkflowInputDefaults_Success(t *testing.T) {
	mem := testfs.NewMemory(t)
	mem.WriteFile(".github/workflows/ok.yml", []byte("name: test\non:\n  workflow_call:\n    inputs:\n      foo:\n        default: literal\n"))
	mem.WriteFile(".github/workflows/nested/ignored.yml", []byte("      default: ${{ secrets.BAD }}\n"))

	var stdout bytes.Buffer
	if err := appvalidate.WorkflowInputDefaults(&stdout, appvalidate.WorkflowInputDefaultsInput{Root: ".", FS: mem.FS()}); err != nil {
		t.Fatalf("WorkflowInputDefaults: %v", err)
	}
	if got := stdout.String(); got != "Workflow input defaults look valid.\n" {
		t.Errorf("stdout = %q", got)
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

	var stdout bytes.Buffer
	err := appvalidate.WorkflowInputDefaults(&stdout, appvalidate.WorkflowInputDefaultsInput{Root: ".", FS: mem.FS()})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "::error file=.github/workflows/bad.yml,line=6::workflow_call input defaults must be literal values") {
		t.Errorf("missing annotation in:\n%s", out)
	}
	if !strings.Contains(out, "default: ${{ github.ref_name }}") {
		t.Errorf("missing offending line in:\n%s", out)
	}
}
