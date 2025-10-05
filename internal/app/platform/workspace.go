// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package platform orchestrates checkout and workspace diagnostics.
package platform

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// DebugWorkspaceInput describes the workspace and diagnostic action context.
type DebugWorkspaceInput struct {
	Root             string // empty defaults to cwd
	ActionRepository string
	ActionRef        string
}

// DebugWorkspace lists only descriptor-rooted workspace entries. It validates
// the complete listing before writing so a bad shared tree cannot half-report.
func DebugWorkspace(out io.Writer, in DebugWorkspaceInput) error { //nolint:cyclop // two rooted listings and a checked script walk complete before output.
	if out == nil {
		return fmt.Errorf("debug-workspace: output writer is required: %w", errs.ErrUsage)
	}

	dir := in.Root
	if dir == "" {
		dir = "."
	}

	root, err := pathsafe.OpenRoot(dir)
	if err != nil {
		return err
	}

	defer func() { _ = root.Close() }()

	var body bytes.Buffer
	fmt.Fprintln(&body, "=== Workspace structure ===")

	if listErr := listDirVerbose(&body, root); listErr != nil {
		return listErr
	}

	fmt.Fprintln(&body, "\n=== .github-shared structure ===")

	var scripts []string

	shared, err := pathsafe.OpenRoot(filepath.Join(root.Name(), ".github-shared"))
	switch {
	case errors.Is(err, os.ErrNotExist):
		fmt.Fprintln(&body, ".github-shared not found")
	case err != nil:
		return err
	default:
		defer func() { _ = shared.Close() }()

		if listErr := listDirVerbose(&body, shared); listErr != nil {
			return listErr
		}

		if walkErr := fs.WalkDir(shared.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			if entry.Type()&fs.ModeSymlink != 0 {
				return fmt.Errorf("workspace diagnostic tree contains a link: %w", errs.ErrValidation)
			}

			if entry.Type().IsRegular() && strings.HasPrefix(entry.Name(), "validate-") && strings.HasSuffix(entry.Name(), ".sh") {
				scripts = append(scripts, filepath.Join(shared.Name(), path))
			}

			return nil
		}); walkErr != nil {
			return walkErr
		}
	}

	fmt.Fprintln(&body, "\n=== Looking for scripts ===")

	for _, path := range scripts {
		fmt.Fprintln(&body, workspaceText(path))
	}

	if len(scripts) == 0 {
		fmt.Fprintln(&body, "No scripts found")
	}

	fmt.Fprintln(&body, "\n=== GitHub context ===")
	fmt.Fprintf(&body, "action_repository: %s\naction_ref: %s\n", workspaceText(in.ActionRepository), workspaceText(in.ActionRef))
	_, err = io.Copy(out, &body)

	return err
}

func listDirVerbose(out io.Writer, root *os.Root) error {
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return err
		}

		if _, err := fmt.Fprintf(out, "%s %8d %s\n", info.Mode(), info.Size(), workspaceText(entry.Name())); err != nil {
			return err
		}
	}

	return nil
}

func workspaceText(value string) string {
	if !utf8.ValidString(value) || strings.ContainsFunc(value, unicode.IsControl) {
		return strconv.QuoteToASCII(value)
	}

	return value
}
