// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/validate"
)

// ymlExt is the workflow file extension this package scans for.
const ymlExt = ".yml"

// JobGraphInput drives JobGraph.
type JobGraphInput struct {
	// Root is the repository root. Empty -> cwd.
	Root string
	// WorkflowsDir overrides the workflows directory. Empty -> <Root>/.github/workflows.
	WorkflowsDir string
	// FS overrides filesystem access for tests. Nil -> the real OS filesystem rooted at Root.
	FS fs.FS
}

// JobGraph scans every reusable workflow for the "reusable consumer masks a
// skipped producer" hazard (a reusable-call job reading a skippable producer's
// outputs) and fails if any unacknowledged edge exists. Violations are emitted
// as error annotations; the call returns a wrapped errs.ErrValidation.
func JobGraph(w io.Writer, annot output.Annotator, in JobGraphInput) error { //nolint:varnamelen // idiomatic short name.
	root := in.Root
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}

		root = cwd
	}

	workflowsDir := in.WorkflowsDir
	if workflowsDir == "" {
		workflowsDir = filepath.Join(root, ".github", "workflows")
	}

	if in.FS != nil {
		return jobGraphInFS(w, annot, in.FS, workflowDirForFS(filepath.ToSlash(filepath.Clean(workflowsDir))))
	}

	relDir, err := filepath.Rel(root, workflowsDir)
	if err != nil {
		return fmt.Errorf("relative workflows dir %s from %s: %w", workflowsDir, root, err)
	}

	return jobGraphInFS(w, annot, os.DirFS(root), filepath.ToSlash(relDir))
}

// collectWorkflowFiles returns the sorted .yml/.yaml workflow paths under
// workflowsDir (directories and other extensions skipped).
func collectWorkflowFiles(entries []fs.DirEntry, workflowsDir string) []string {
	files := make([]string, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() || (filepath.Ext(entry.Name()) != ymlExt && filepath.Ext(entry.Name()) != ".yaml") {
			continue
		}

		files = append(files, path.Join(workflowsDir, entry.Name()))
	}

	sort.Strings(files)

	return files
}

func jobGraphInFS(w io.Writer, annot output.Annotator, fsys fs.FS, workflowsDir string) error { //nolint:varnamelen // idiomatic short name.
	entries, err := fs.ReadDir(fsys, workflowsDir)
	if err != nil {
		return fmt.Errorf("read workflows dir %s: %w", workflowsDir, err)
	}

	files := collectWorkflowFiles(entries, workflowsDir)

	failures := 0

	for _, file := range files {
		data, err := fs.ReadFile(fsys, file)
		if err != nil {
			return fmt.Errorf("read %s: %w", file, err)
		}

		violations, err := validate.CheckJobGraph(data)
		if err != nil {
			return fmt.Errorf("check %s: %w", file, err)
		}

		for _, v := range violations {
			annot.ErrorAt(output.Annotation{File: file, Line: v.Line}, "%s", v.Msg)

			failures++
		}
	}

	if failures > 0 {
		return fmt.Errorf("job-graph masking validation failed: %w", errs.ErrValidation)
	}

	_, _ = fmt.Fprintln(w, "No reusable-consumer job-graph masking risks.")

	return nil
}
