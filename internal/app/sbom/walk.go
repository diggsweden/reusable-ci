// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package sbom

// Filesystem walk primitives used by the per-project artifact-discovery
// helpers in discovery.go. Each walker mirrors a bash `find …` expression
// and tolerates per-entry errors (logged via slog, walk continues) so a
// single unreadable file doesn't fail the whole SBOM step.

import (
	"io/fs"
	"log/slog"
	"strings"
)

// walkMatching returns every regular file under root where predicate
// returns true. Returns nil when root doesn't exist.
func walkMatching(ws workspace, root string, predicate func(basename string) bool) []string {
	if _, err := ws.stat(root); err != nil {
		return nil
	}
	var out []string
	_ = fs.WalkDir(ws.fsys, cleanFSPath(root), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("walkFilesWithFilter: skipping unreadable entry", "path", path, "err", err)
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if predicate(d.Name()) {
			out = append(out, path)
		}
		return nil
	})
	return out
}

// walkExecutable returns regular files whose owner-executable bit is set.
func walkExecutable(ws workspace, root string) []string {
	return walkExecutableWithFilter(ws, root, nil)
}

// walkExecutableNoDebugInfo excludes *.d files (cargo's debug-info
// outputs under target/release).
func walkExecutableNoDebugInfo(ws workspace, root string) []string {
	return walkExecutableWithFilter(ws, root, func(name string) bool {
		return !strings.HasSuffix(name, ".d")
	})
}

func walkExecutableWithFilter(ws workspace, root string, filter func(name string) bool) []string {
	if _, err := ws.stat(root); err != nil {
		return nil
	}
	var out []string
	_ = fs.WalkDir(ws.fsys, cleanFSPath(root), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("walkExecutable: skipping unreadable entry", "path", path, "err", err)
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			slog.Warn("walkExecutable: skipping entry with unreadable metadata", "path", path, "err", err)
			return nil
		}
		// 0o100 = owner-executable bit, like find -executable.
		if info.Mode().Perm()&0o100 == 0 {
			return nil
		}
		if filter != nil && !filter(d.Name()) {
			return nil
		}
		out = append(out, path)
		return nil
	})
	return out
}
