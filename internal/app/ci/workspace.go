// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package ci hosts misc CI-platform helpers (debug listings, workspace
// inspection) that aren't part of any single domain.
package ci

import (
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// DebugWorkspaceInput drives DebugWorkspace.
type DebugWorkspaceInput struct {
	// Root is the workspace root. Empty → cwd.
	Root string
	// ActionRepository / ActionRef are passed through from the workflow
	// for reproducibility ($ACTION_REPOSITORY / $ACTION_REF).
	ActionRepository string
	ActionRef        string
}

// DebugWorkspace prints the workspace listing, the .github-shared/
// listing if present, validate-* script paths, and the github action
// context. Mirrors scripts/ci/debug-workspace.sh.
func DebugWorkspace(stdout io.Writer, in DebugWorkspaceInput) error {
	root := in.Root
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
	}

	fmt.Fprintln(stdout, "=== Workspace structure ===")
	listDirVerbose(stdout, root)

	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "=== .github-shared structure ===")
	shared := filepath.Join(root, ".github-shared")
	if _, err := os.Stat(shared); err == nil {
		listDirVerbose(stdout, shared)
	} else {
		fmt.Fprintln(stdout, ".github-shared not found")
	}

	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "=== Looking for scripts ===")
	any := false
	if _, err := os.Stat(shared); err == nil {
		_ = filepath.WalkDir(shared, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				slog.Warn("DebugWorkspace: skipping unreadable entry", "path", path, "err", err)
				return nil
			}
			if d.IsDir() {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, "validate-") && strings.HasSuffix(name, ".sh") {
				fmt.Fprintln(stdout, path)
				any = true
			}
			return nil
		})
	}
	if !any {
		fmt.Fprintln(stdout, "No scripts found")
	}

	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "=== GitHub context ===")
	fmt.Fprintf(stdout, "action_repository: %s\n", in.ActionRepository)
	fmt.Fprintf(stdout, "action_ref: %s\n", in.ActionRef)
	return nil
}

// listDirVerbose prints a `ls -la`-shaped listing. The bash uses real
// `ls -la` output for visual fidelity; this is a coarser equivalent
// (mode + size + name) since the Go port doesn't need pixel-perfect
// match for a debug helper.
func listDirVerbose(out io.Writer, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(out, "(error reading %s: %v)\n", dir, err)
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			fmt.Fprintf(out, "?  %s\n", e.Name())
			continue
		}
		fmt.Fprintf(out, "%s %8d %s\n", info.Mode(), info.Size(), e.Name())
	}
}
