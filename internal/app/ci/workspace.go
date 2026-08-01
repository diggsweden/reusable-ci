// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

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
// context.
func DebugWorkspace(w io.Writer, in DebugWorkspaceInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	root := in.Root
	if root == "" {
		var err error

		root, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
	}

	_, _ = fmt.Fprintln(w, "=== Workspace structure ===")
	listDirVerbose(w, root)

	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "=== .github-shared structure ===")

	shared := filepath.Join(root, ".github-shared")
	if _, err := os.Stat(shared); err == nil {
		listDirVerbose(w, shared)
	} else {
		_, _ = fmt.Fprintln(w, ".github-shared not found")
	}

	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "=== Looking for scripts ===")

	found := false

	if _, err := os.Stat(shared); err == nil {
		_ = filepath.WalkDir(shared, func(path string, d fs.DirEntry, err error) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
			if err != nil {
				slog.Debug("DebugWorkspace: skipping unreadable entry", "path", path, "err", err)

				return nil
			}

			if d.IsDir() {
				return nil
			}

			name := d.Name()
			if strings.HasPrefix(name, "validate-") && strings.HasSuffix(name, ".sh") {
				_, _ = fmt.Fprintln(w, path)

				found = true
			}

			return nil
		})
	}

	if !found {
		_, _ = fmt.Fprintln(w, "No scripts found")
	}

	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "=== GitHub context ===")
	_, _ = fmt.Fprintf(w, "action_repository: %s\n", in.ActionRepository)
	_, _ = fmt.Fprintf(w, "action_ref: %s\n", in.ActionRef)

	return nil
}

// listDirVerbose prints a `ls -la`-shaped listing. The bash uses real
// `ls -la` output for visual fidelity; this is a coarser equivalent
// (mode + size + name) since the Go port doesn't need pixel-perfect
// match for a debug helper.
func listDirVerbose(out io.Writer, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		_, _ = fmt.Fprintf(out, "(error reading %s: %v)\n", dir, err)

		return
	}

	for _, e := range entries { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		info, err := e.Info()
		if err != nil {
			_, _ = fmt.Fprintf(out, "?  %s\n", e.Name())

			continue
		}

		_, _ = fmt.Fprintf(out, "%s %8d %s\n", info.Mode(), info.Size(), e.Name())
	}
}
