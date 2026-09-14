// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"gopkg.in/yaml.v3"
)

// workflowDefaultsDocument reads only the trigger block. GitHub also accepts
// `on: push` and `on: [push, pull_request]`; neither shape can declare
// workflow_call inputs, so only a mapping is decoded further.
type workflowDefaultsDocument struct {
	On yaml.Node `yaml:"on"`
}

type workflowCallTrigger struct {
	WorkflowCall struct {
		Inputs map[string]struct {
			Default yaml.Node `yaml:"default"`
		} `yaml:"inputs"`
	} `yaml:"workflow_call"`
}

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
func WorkflowInputDefaults(w io.Writer, annot output.Annotator, in WorkflowInputDefaultsInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	fsys, workflowsDir, err := workflowScanDir(in.Root, in.WorkflowsDir, in.FS)
	if err != nil {
		return err
	}

	return workflowInputDefaultsInFS(w, annot, workflowInputDefaultsFSInput{
		workflowsDir: workflowsDir,
		fsys:         fsys,
	})
}

// workflowScanDir returns the filesystem and slash-separated directory a
// workflow directory scan reads, with reported paths relative to the root. An
// empty root is the working directory and an empty directory is
// <root>/.github/workflows; a relative directory is taken from the working
// directory, as a path flag is. The OS filesystem is rooted at root, so a
// directory outside it is refused as usage rather than failing to read "..".
func workflowScanDir(root, workflowsDir string, fsys fs.FS) (fs.FS, string, error) {
	if root == "" {
		root = "."
	}

	if workflowsDir == "" {
		workflowsDir = filepath.Join(root, ".github", "workflows")
	}

	if fsys != nil {
		return fsys, workflowDirForFS(filepath.ToSlash(filepath.Clean(workflowsDir))), nil
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, "", fmt.Errorf("resolve root %s: %w: %w", root, err, errs.ErrUsage)
	}

	absDir, err := filepath.Abs(workflowsDir)
	if err != nil {
		return nil, "", fmt.Errorf("resolve workflows dir %s: %w: %w", workflowsDir, err, errs.ErrUsage)
	}

	rel, err := filepath.Rel(absRoot, absDir)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, "", fmt.Errorf("workflows dir %s is not inside root %s: %w", workflowsDir, root, errs.ErrUsage)
	}

	return os.DirFS(absRoot), filepath.ToSlash(rel), nil
}

func workflowInputDefaultsInFS(w io.Writer, annot output.Annotator, in workflowInputDefaultsFSInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	entries, err := fs.ReadDir(in.fsys, in.workflowsDir)
	if err != nil {
		return workflowReadError(in.workflowsDir, err)
	}

	failures := 0

	for _, path := range collectWorkflowFiles(entries, in.workflowsDir) {
		count, err := scanWorkflowInputDefaults(annot, in.fsys, path)
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

func scanWorkflowInputDefaults(annot output.Annotator, fsys fs.FS, path string) (int, error) {
	body, err := fs.ReadFile(fsys, path)
	if err != nil {
		return 0, workflowReadError(path, err)
	}

	defaults, err := expressionDefaults(body)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w: %w", path, err, errs.ErrMalformedInput)
	}

	lines := strings.Split(string(body), "\n")

	for _, defaultNode := range defaults {
		line := ""
		if defaultNode.Line <= len(lines) {
			line = lines[defaultNode.Line-1]
		}

		// The Annotator escapes the file path and message; on non-GitHub
		// runners it renders a plain "Error: <file>:<line>: …" instead of a
		// workflow command.
		annot.ErrorAt(output.Annotation{File: path, Line: defaultNode.Line},
			"workflow_call input defaults must be literal values, found expression: %s", line)
	}

	return len(defaults), nil
}

// expressionDefaults returns the workflow_call input defaults in body that
// hold an expression, in line order.
func expressionDefaults(body []byte) ([]yaml.Node, error) {
	var workflow workflowDefaultsDocument
	if err := yaml.Unmarshal(body, &workflow); err != nil {
		return nil, err
	}

	var trigger workflowCallTrigger

	if on := &workflow.On; on.Kind == yaml.MappingNode || (on.Kind == yaml.AliasNode && on.Alias != nil && on.Alias.Kind == yaml.MappingNode) {
		if err := on.Decode(&trigger); err != nil {
			return nil, err
		}
	}

	defaults := make([]yaml.Node, 0, len(trigger.WorkflowCall.Inputs))
	for _, input := range trigger.WorkflowCall.Inputs {
		if input.Default.Line > 0 && yamlNodeContainsExpression(&input.Default) {
			defaults = append(defaults, input.Default)
		}
	}

	sort.Slice(defaults, func(i, j int) bool { return defaults[i].Line < defaults[j].Line })

	return defaults, nil
}

// yamlNodeContainsExpression reports whether any scalar reachable from node,
// through aliases, holds an expression. walkYAML visits each node once, so a
// self-referencing anchor ends the walk instead of recursing without bound.
func yamlNodeContainsExpression(node *yaml.Node) bool {
	found := false

	walkYAML(node, map[*yaml.Node]bool{}, func(visited *yaml.Node) {
		if visited.Kind == yaml.ScalarNode && strings.Contains(visited.Value, "${{") {
			found = true
		}
	})

	return found
}

func workflowDirForFS(dir string) string {
	if dir == "" || dir == "." {
		return "."
	}

	return strings.TrimPrefix(dir, "./")
}
