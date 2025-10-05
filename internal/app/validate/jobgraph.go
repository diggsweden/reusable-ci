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

// Workflow file extensions scanned by validators in this package.
const (
	ymlExt  = ".yml"
	yamlExt = ".yaml"
)

// JobGraphInput drives JobGraph.
type JobGraphInput struct {
	// Root is the repository root. Empty -> cwd.
	Root string
	// WorkflowsDir overrides the workflows directory. Empty -> <Root>/.github/workflows.
	WorkflowsDir string
	// Workflows, when set, is the exact workflow file list to check instead of
	// scanning WorkflowsDir. Paths are interpreted relative to Root unless absolute.
	Workflows []string
	// FS overrides filesystem access for tests. Nil -> the real OS filesystem rooted at Root.
	FS fs.FS
}

// JobGraph scans every reusable workflow for the "reusable consumer masks a
// skipped producer" hazard (a reusable-call job reading a skippable producer's
// outputs) and fails if any unacknowledged edge exists. Violations are emitted
// as error annotations; the call returns a wrapped errs.ErrValidation.
func JobGraph(w io.Writer, annot output.Annotator, in JobGraphInput) error { //nolint:varnamelen // idiomatic short name.
	if len(in.Workflows) > 0 {
		if in.FS != nil {
			return jobGraphFiles(w, annot, workflowFilesForFS(in.Workflows), func(file string) ([]byte, error) {
				return fs.ReadFile(in.FS, file)
			})
		}

		files := make([]string, 0, len(in.Workflows))
		for _, file := range in.Workflows {
			files = append(files, filepath.ToSlash(filepath.Clean(file)))
		}

		return jobGraphFiles(w, annot, files, func(file string) ([]byte, error) {
			diskPath := filepath.FromSlash(file)
			if !filepath.IsAbs(diskPath) {
				diskPath = filepath.Join(in.Root, diskPath)
			}

			return os.ReadFile(diskPath) //nolint:gosec // operator-supplied workflow path.
		})
	}

	fsys, workflowsDir, err := workflowScanDir(in.Root, in.WorkflowsDir, in.FS)
	if err != nil {
		return err
	}

	entries, err := fs.ReadDir(fsys, workflowsDir)
	if err != nil {
		return workflowReadError(workflowsDir, err)
	}

	return jobGraphFiles(w, annot, collectWorkflowFiles(entries, workflowsDir), func(file string) ([]byte, error) {
		return fs.ReadFile(fsys, file)
	})
}

func workflowFilesForFS(files []string) []string {
	out := make([]string, 0, len(files))
	for _, file := range files {
		out = append(out, workflowDirForFS(filepath.ToSlash(filepath.Clean(file))))
	}

	return out
}

// jobGraphFiles checks every file in order and reports every violation. A file
// that cannot be read or parsed stops the run with its own class: the result
// could no longer claim to cover the requested workflows.
func jobGraphFiles(w io.Writer, annot output.Annotator, files []string, read func(string) ([]byte, error)) error { //nolint:varnamelen // idiomatic short name.
	failures := 0

	for _, file := range files {
		data, err := read(file)
		if err != nil {
			return workflowReadError(file, err)
		}

		count, err := checkJobGraphFile(annot, file, data)
		if err != nil {
			return err
		}

		failures += count
	}

	if failures > 0 {
		return fmt.Errorf("job-graph masking validation failed: %w", errs.ErrValidation)
	}

	_, _ = fmt.Fprintln(w, "No reusable-consumer job-graph masking risks.")

	return nil
}

// collectWorkflowFiles returns the sorted .yml/.yaml workflow paths under
// workflowsDir (directories and other extensions skipped).
func collectWorkflowFiles(entries []fs.DirEntry, workflowsDir string) []string {
	files := make([]string, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() || (filepath.Ext(entry.Name()) != ymlExt && filepath.Ext(entry.Name()) != yamlExt) {
			continue
		}

		files = append(files, path.Join(workflowsDir, entry.Name()))
	}

	sort.Strings(files)

	return files
}

func checkJobGraphFile(annot output.Annotator, file string, data []byte) (int, error) {
	violations, err := validate.CheckJobGraph(data)
	if err != nil {
		return 0, fmt.Errorf("check %s: %w: %w", file, err, errs.ErrMalformedInput)
	}

	for _, v := range violations {
		annot.ErrorAt(output.Annotation{File: file, Line: v.Line}, "%s", v.Msg)
	}

	return len(violations), nil
}
