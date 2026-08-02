// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNoBPOEnvNamespaceOutsideShim keeps the removed BPO_-prefixed env
// namespace out of the tree. It was private to one command and drifted
// from the container group's shared env names; the canonical names
// replaced it and the namespace was deleted — nothing may read or set a
// BPO_ var again. Only source and workflow files are scanned.
func TestNoBPOEnvNamespaceOutsideShim(t *testing.T) {
	t.Parallel()

	// Only this guard itself may mention the dead namespace.
	bpoAllowedFiles := map[string]bool{
		"internal/cli/envnames_guard_test.go": true,
	}

	root := repoRoot(t)

	var offenders []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		name := entry.Name()
		if entry.IsDir() {
			if name == ".git" || name == "dist" || name == "node_modules" {
				return filepath.SkipDir
			}

			return nil
		}

		switch filepath.Ext(name) {
		case ".go", ".yml", ".yaml", ".sh":
		default:
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		if bpoAllowedFiles[filepath.ToSlash(rel)] {
			return nil
		}

		content, readErr := os.ReadFile(path) //nolint:gosec // test walks repo-local files.
		if readErr != nil {
			return readErr
		}

		if strings.Contains(string(content), "BPO_") {
			offenders = append(offenders, filepath.ToSlash(rel))
		}

		return nil
	})
	require.NoError(t, err)

	require.Emptyf(t, offenders,
		"deprecated BPO_ env vars referenced outside the buildpush shim; "+
			"use the canonical names (see bpoEnvAliases in buildpush.go)")
}
