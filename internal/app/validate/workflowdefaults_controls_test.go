// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func workflowYAML(lines ...string) []byte {
	return []byte(strings.Join(lines, "\n") + "\n")
}

// TestWorkflowInputDefaults_ReportsEveryOffendingDefault spreads offending
// defaults over two files, each after a clean input: a plain expression, one
// reached through an alias, one nested inside a sequence, and one inside a
// flow mapping, plus a trigger block reached through an alias. Every one is
// reported with its file and line, files in path order and lines ascending.
// Literal defaults, expressions in descriptions, other triggers, nested
// directories, and the scalar and sequence forms of `on` stay accepted and
// silent; the scalar form used to refuse the whole run as malformed.
func TestWorkflowInputDefaults_ReportsEveryOffendingDefault(t *testing.T) {
	mem := testfs.NewMemory(t)
	mem.WriteFile(".github/workflows/b.yaml", workflowYAML(
		"x-shared: &expr '${{ github.sha }}'",
		"on:",
		"  workflow_call:",
		"    inputs:",
		"      clean:",
		"        description: ${{ ignored }}",
		"        default: literal",
		"      aliased:",
		"        default: *expr",
		"      nested:",
		"        default: [one, '${{ inputs.two }}']",
	))
	mem.WriteFile(".github/workflows/a.yml", workflowYAML(
		"on:",
		"  workflow_call:",
		"    inputs: {clean: {default: 'x'}, flow: {default: '${{ vars.X }}'}}",
		"  push:",
	))
	mem.WriteFile(".github/workflows/c.yml", workflowYAML(
		"on:",
		"  workflow_call:",
		"    inputs:",
		"      plain:",
		"        default: ${{ github.ref_name }}",
	))
	mem.WriteFile(".github/workflows/scalar.yml", workflowYAML("on: push", "jobs: {}"))
	mem.WriteFile(".github/workflows/sequence.yml", workflowYAML("on: [push, workflow_call]", "jobs: {}"))
	mem.WriteFile(".github/workflows/bare.yml", workflowYAML("on: workflow_call", "jobs: {}"))
	mem.WriteFile(".github/workflows/aliased.yml", workflowYAML(
		"x-trigger: &trigger",
		"  workflow_call:",
		"    inputs:",
		"      ok:",
		"        default: ${{ github.actor }}",
		"on: *trigger",
	))
	mem.WriteFile(".github/workflows/dispatch.yml", workflowYAML(
		"on:",
		"  workflow_dispatch:",
		"    inputs:",
		"      foo:",
		"        default: ${{ github.ref_name }}",
	))
	mem.WriteFile(".github/workflows/nested.yml/deep.yml", []byte("on:\n  workflow_call:\n    inputs:\n      a:\n        default: ${{ x }}\n"))

	var out, annot bytes.Buffer

	err := appvalidate.WorkflowInputDefaults(&out, output.NewAnnotator(&annot, output.FormatGitHub), appvalidate.WorkflowInputDefaultsInput{Root: ".", FS: mem.FS()})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation\n%s", err, annot.String())
	}

	want := []string{
		"file=.github/workflows/a.yml,line=3",
		"file=.github/workflows/aliased.yml,line=5",
		"file=.github/workflows/b.yaml,line=9",
		"file=.github/workflows/b.yaml,line=11",
		"file=.github/workflows/c.yml,line=5",
	}
	if got := annotationTargets(annot.String()); !slices.Equal(got, want) {
		t.Errorf("annotations = %q, want %q\n%s", got, want, annot.String())
	}

	if out.Len() != 0 {
		t.Errorf("a failed check reported success: %q", out.String())
	}
}

// TestWorkflowInputDefaults_HostileAndUnreadableFiles covers files the scan
// must survive or refuse by class. A self-referencing anchor under a default is
// walked once, reported when it holds an expression and accepted when it does
// not; it used to recurse until the process died. A later file that is not
// YAML, or whose trigger block is not a trigger shape, is malformed input after
// the earlier file's finding is reported.
func TestWorkflowInputDefaults_HostileAndUnreadableFiles(t *testing.T) {
	const earlier = "on:\n  workflow_call:\n    inputs:\n      a:\n        default: ${{ x }}\n"

	for _, tc := range []struct {
		name        string
		body        string
		want        error
		wantTargets []string
	}{
		{
			name:        "cycle holding an expression",
			body:        "on:\n  workflow_call:\n    inputs:\n      a: &loop\n        description: '${{ inside }}'\n        default: [*loop]\n",
			want:        errs.ErrValidation,
			wantTargets: []string{"file=.github/workflows/a.yml,line=5", "file=.github/workflows/b.yml,line=6"},
		},
		{
			name:        "cycle without an expression",
			body:        "on:\n  workflow_call:\n    inputs:\n      a: &loop\n        default: [*loop]\n",
			want:        errs.ErrValidation,
			wantTargets: []string{"file=.github/workflows/a.yml,line=5"},
		},
		{
			name:        "not yaml",
			body:        "on: [unterminated\n",
			want:        errs.ErrMalformedInput,
			wantTargets: []string{"file=.github/workflows/a.yml,line=5"},
		},
		{
			name:        "workflow_call as a sequence",
			body:        "on:\n  workflow_call: [inputs]\n",
			want:        errs.ErrMalformedInput,
			wantTargets: []string{"file=.github/workflows/a.yml,line=5"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mem := testfs.NewMemory(t)
			mem.WriteFile(".github/workflows/a.yml", []byte(earlier))
			mem.WriteFile(".github/workflows/b.yml", []byte(tc.body))

			var out, annot bytes.Buffer

			err := appvalidate.WorkflowInputDefaults(&out, output.NewAnnotator(&annot, output.FormatGitHub), appvalidate.WorkflowInputDefaultsInput{Root: ".", FS: mem.FS()})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}

			if !errors.Is(tc.want, errs.ErrValidation) && (errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), ".github/workflows/b.yml")) {
				t.Errorf("err = %v, want only the later file's own class, naming it", err)
			}

			if got := annotationTargets(annot.String()); !slices.Equal(got, tc.wantTargets) {
				t.Errorf("annotations = %q, want %q", got, tc.wantTargets)
			}

			if out.Len() != 0 {
				t.Errorf("a failed check reported success: %q", out.String())
			}
		})
	}

	t.Run("later file is a link to nothing", func(t *testing.T) {
		repo := testfs.NewReal(t)
		repo.WriteFile(".github/workflows/a.yml", []byte(earlier))

		if err := os.Symlink(repo.Path("nowhere.yml"), repo.Path(".github/workflows/b.yml")); err != nil {
			t.Fatal(err)
		}

		var out, annot bytes.Buffer

		err := appvalidate.WorkflowInputDefaults(&out, output.NewAnnotator(&annot, output.FormatGitHub), appvalidate.WorkflowInputDefaultsInput{Root: repo.Root})
		if !errors.Is(err, errs.ErrMissingInput) || errors.Is(err, errs.ErrValidation) {
			t.Fatalf("err = %v, want ErrMissingInput only", err)
		}

		if got := annotationTargets(annot.String()); !slices.Equal(got, []string{"file=.github/workflows/a.yml,line=5"}) || out.Len() != 0 {
			t.Errorf("annotations = %q out = %q, want the earlier finding and no success", got, out.String())
		}
	})
}
