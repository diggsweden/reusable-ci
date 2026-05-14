// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package sbom

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type workspace struct {
	root string
	fsys fs.FS
	cwd  string
}

func newWorkspace(in GenerateInput) (workspace, error) {
	dir := in.WorkingDir
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return workspace{}, fmt.Errorf("abs %s: %w", dir, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return workspace{}, fmt.Errorf("working directory %s: %w", abs, err)
	}
	if !info.IsDir() {
		return workspace{}, fmt.Errorf("working directory %s is not a directory", abs)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return workspace{}, fmt.Errorf("getwd: %w", err)
	}
	fsys := in.FS
	if fsys == nil {
		fsys = os.DirFS(abs)
	}
	return workspace{root: abs, fsys: fsys, cwd: cwd}, nil
}

func (w workspace) readFile(name string) ([]byte, error) {
	return fs.ReadFile(w.fsys, cleanFSPath(name))
}

func (w workspace) readDir(name string) ([]fs.DirEntry, error) {
	return fs.ReadDir(w.fsys, cleanFSPath(name))
}

func (w workspace) stat(name string) (fs.FileInfo, error) {
	return fs.Stat(w.fsys, cleanFSPath(name))
}

func (w workspace) open(name string) (fs.File, error) {
	return w.fsys.Open(cleanFSPath(name))
}

func (w workspace) outputPath(name string) string {
	clean := cleanFSPath(name)
	if clean == "." {
		return w.root
	}
	if w.rootIsCwd() {
		return filepath.FromSlash(clean)
	}
	return filepath.Join(w.root, filepath.FromSlash(clean))
}

func (w workspace) scanTarget(name string) string {
	clean := cleanFSPath(name)
	if clean == "." {
		if w.rootIsCwd() {
			return "."
		}
		return w.root
	}
	if w.rootIsCwd() {
		return filepath.FromSlash(clean)
	}
	return filepath.Join(w.root, filepath.FromSlash(clean))
}

func (w workspace) rootIsCwd() bool {
	rel, err := filepath.Rel(w.cwd, w.root)
	return err == nil && rel == "."
}

func (w workspace) base() string {
	return filepath.Base(w.root)
}

func cleanFSPath(name string) string {
	name = filepath.ToSlash(filepath.Clean(name))
	name = strings.TrimPrefix(name, "./")
	name = strings.TrimPrefix(name, "/")
	if name == "" || name == "." {
		return "."
	}
	return path.Clean(name)
}

func relFromWalkRoot(root, name string) string {
	root = cleanFSPath(root)
	name = cleanFSPath(name)
	if root == "." {
		return name
	}
	return strings.TrimPrefix(name, root+"/")
}
