// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

var workflowInputDefaultPattern = regexp.MustCompile(`^[[:space:]]+default:[[:space:]].*\$\{\{`)

// WorkflowInputDefaultsInput drives WorkflowInputDefaults.
type WorkflowInputDefaultsInput struct {
	// Root is the repository root. Empty -> cwd.
	Root string
	// WorkflowsDir overrides the workflows directory. Empty -> <Root>/.github/workflows.
	WorkflowsDir string
	// FS overrides filesystem access for tests. When nil, the real OS filesystem
	// rooted at Root is used.
	FS fs.FS
}

type workflowInputDefaultsFSInput struct {
	workflowsDir string
	fsys         fs.FS
}

// WorkflowInputDefaults verifies that reusable workflow_call input
// defaults are literal values, not GitHub expression blocks (which
// silently degrade to the empty string when callers don't override
// them).
func WorkflowInputDefaults(w io.Writer, in WorkflowInputDefaultsInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	root := in.Root
	if root == "" {
		var err error

		root, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
	}

	workflowsDir := in.WorkflowsDir
	if workflowsDir == "" {
		workflowsDir = filepath.Join(root, ".github", "workflows")
	}

	if in.FS != nil {
		workflowsDir = filepath.ToSlash(filepath.Clean(workflowsDir))

		return workflowInputDefaultsInFS(w, workflowInputDefaultsFSInput{
			workflowsDir: workflowDirForFS(workflowsDir),
			fsys:         in.FS,
		})
	}

	relDir, err := filepath.Rel(root, workflowsDir)
	if err != nil {
		return fmt.Errorf("relative workflows dir %s from %s: %w", workflowsDir, root, err)
	}

	return workflowInputDefaultsInFS(w, workflowInputDefaultsFSInput{
		workflowsDir: filepath.ToSlash(relDir),
		fsys:         os.DirFS(root),
	})
}

func workflowInputDefaultsInFS(w io.Writer, in workflowInputDefaultsFSInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	entries, err := fs.ReadDir(in.fsys, in.workflowsDir)
	if err != nil {
		return fmt.Errorf("read workflows dir %s: %w", in.workflowsDir, err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yml" {
			continue
		}

		files = append(files, path.Join(in.workflowsDir, entry.Name()))
	}

	sort.Strings(files)

	failures := 0

	for _, path := range files {
		count, err := scanWorkflowInputDefaults(w, in.fsys, path)
		if err != nil {
			return err
		}

		failures += count
	}

	if failures > 0 {
		return fmt.Errorf("workflow input defaults validation failed: %w", errs.ErrValidation)
	}

	_, _ = fmt.Fprintln(w, "Workflow input defaults look valid.")

	return nil
}

func scanWorkflowInputDefaults(w io.Writer, fsys fs.FS, path string) (int, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	file, err := fsys.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", path, err)
	}

	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	lineNo := 0
	failures := 0

	for scanner.Scan() {
		lineNo++

		line := scanner.Text()
		if !workflowInputDefaultPattern.MatchString(line) {
			continue
		}

		_, _ = fmt.Fprintf(w,
			"::error file=%s,line=%d::workflow_call input defaults must be literal values, found expression: %s\n",
			path, lineNo, line)
		failures++
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("scan %s: %w", path, err)
	}

	return failures, nil
}

func workflowDirForFS(dir string) string {
	if dir == "" || dir == "." {
		return "."
	}

	return strings.TrimPrefix(dir, "./")
}
