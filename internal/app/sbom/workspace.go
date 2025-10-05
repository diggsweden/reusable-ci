// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

type workspace struct {
	root   string
	fsys   fs.FS
	cwd    string
	hostFS bool
	host   *os.Root
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

	root, err := pathsafe.OpenRoot(abs)
	if err != nil {
		return workspace{}, fmt.Errorf("working directory %s: %w", abs, err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		_ = root.Close()

		return workspace{}, fmt.Errorf("getwd: %w", err)
	}

	fsys := in.FS

	hostFS := fsys == nil
	if fsys == nil {
		fsys = root.FS()
	}

	return workspace{root: abs, fsys: fsys, cwd: cwd, hostFS: hostFS, host: root}, nil
}

func (w workspace) close() error {
	if w.host == nil {
		return nil
	}

	return w.host.Close()
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

func (w workspace) lstat(name string) (fs.FileInfo, error) {
	if w.hostFS {
		return w.host.Lstat(cleanFSPath(name))
	}

	return fs.Stat(w.fsys, cleanFSPath(name))
}

func (w workspace) openInputRegular(name string) (fs.File, error) {
	if w.hostFS {
		return w.openOutputRegular(name)
	}

	clean := cleanFSPath(name)

	info, err := fs.Stat(w.fsys, clean)
	if err != nil {
		return nil, err
	}

	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("SBOM input is not a regular file: %s: %w", name, errs.ErrValidation)
	}

	file, err := w.fsys.Open(clean)
	if err != nil {
		return nil, err
	}

	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()

		return nil, err
	}

	if !opened.Mode().IsRegular() {
		_ = file.Close()

		return nil, fmt.Errorf("SBOM input changed while opening: %s: %w", name, errs.ErrValidation)
	}

	return file, nil
}

func (w workspace) openOutputRegular(name string) (*os.File, error) {
	clean := cleanFSPath(name)

	info, err := w.host.Lstat(clean)
	if err != nil {
		return nil, err
	}

	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("SBOM input is not a regular file: %s: %w", name, errs.ErrValidation)
	}

	file, err := w.host.Open(clean)
	if err != nil {
		return nil, err
	}

	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()

		return nil, err
	}

	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		_ = file.Close()

		return nil, fmt.Errorf("SBOM input changed while opening: %s: %w", name, errs.ErrValidation)
	}

	return file, nil
}

func (w workspace) createOutput(name string, perm fs.FileMode) (*os.File, error) {
	clean := cleanFSPath(name)

	info, err := w.host.Lstat(clean)
	if err == nil {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("SBOM output is not a regular file: %s: %w", name, errs.ErrValidation)
		}

		if removeErr := w.host.Remove(clean); removeErr != nil {
			return nil, removeErr
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	return w.host.OpenFile(clean, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
}

func (w workspace) removeOutput(name string) error {
	return w.host.Remove(cleanFSPath(name))
}

func (w workspace) outputInfo(name string) (fs.FileInfo, error) {
	return w.host.Lstat(cleanFSPath(name))
}

func (w workspace) outputEntries(name string) ([]fs.DirEntry, error) {
	return fs.ReadDir(w.host.FS(), cleanFSPath(name))
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
