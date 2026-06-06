// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package testfs provides a thin filesystem helper for tests. It supports
// both real temp-dir-backed paths and in-memory fs.FS fixtures for pure,
// file-heavy logic.
package testfs

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// Real is a temp-dir-backed filesystem helper for tests that need real OS
// paths because the production code under test uses os.* APIs directly.
type Real struct {
	t    *testing.T
	Root string
}

// NewReal returns a temp-dir-backed filesystem helper.
func NewReal(t *testing.T) *Real {
	t.Helper()

	return &Real{t: t, Root: t.TempDir()}
}

// Path returns an absolute path under the helper's temp root.
func (r *Real) Path(parts ...string) string {
	all := append([]string{r.Root}, parts...)

	return filepath.Join(all...)
}

// MkdirAll creates a directory tree under the temp root and returns it.
func (r *Real) MkdirAll(parts ...string) string {
	r.t.Helper()

	p := r.Path(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil { //nolint:gosec // test infra; t.TempDir() is the root.
		r.t.Fatalf("testfs: mkdir %q: %v", p, err)
	}

	return p
}

// Chdir changes the process working directory to the helper's temp root for
// the duration of the test.
//
// Because cwd is process-global, callers must not use t.Parallel at the same
// scope when calling Chdir.
func (r *Real) Chdir() {
	r.t.Helper()

	cwd, err := os.Getwd()
	if err != nil {
		r.t.Fatalf("testfs: getwd: %v", err)
	}

	if err := os.Chdir(r.Root); err != nil {
		r.t.Fatalf("testfs: chdir %q: %v", r.Root, err)
	}

	r.t.Cleanup(func() { _ = os.Chdir(cwd) })
}

// WriteFile writes a file under the temp root and returns its absolute path.
func (r *Real) WriteFile(rel string, body []byte) string {
	r.t.Helper()

	p := r.Path(rel) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil { //nolint:gosec // test infra; t.TempDir() is the root.
		r.t.Fatalf("testfs: mkdir parent for %q: %v", p, err)
	}

	if err := os.WriteFile(p, body, 0o644); err != nil { //nolint:gosec // test infra; t.TempDir() is the root.
		r.t.Fatalf("testfs: write %q: %v", p, err)
	}

	return p
}

// ReadFile reads a file under the temp root.
func (r *Real) ReadFile(rel string) []byte {
	r.t.Helper()
	p := r.Path(rel)

	body, err := os.ReadFile(p) //nolint:gosec // test infra; p is r.Root-rooted.
	if err != nil {
		r.t.Fatalf("testfs: read %q: %v", p, err)
	}

	return body
}

// Memory is an fstest.MapFS-backed helper for pure code that accepts fs.FS.
type Memory struct {
	t  *testing.T
	fs fstest.MapFS
}

// NewMemory returns an empty in-memory filesystem helper.
func NewMemory(t *testing.T) *Memory {
	t.Helper()

	return &Memory{t: t, fs: fstest.MapFS{}}
}

// FS returns the helper as a read-only fs.FS.
func (m *Memory) FS() fs.FS { return m.fs }

// MkdirAll records a directory path in the in-memory filesystem.
func (m *Memory) MkdirAll(rel string) {
	m.t.Helper()

	for _, dir := range parentDirs(clean(rel)) {
		if dir == "." || dir == "" {
			continue
		}

		if _, ok := m.fs[dir]; !ok {
			m.fs[dir] = &fstest.MapFile{Mode: fs.ModeDir | 0o755}
		}
	}

	if rel != "" {
		dir := clean(rel)
		if _, ok := m.fs[dir]; !ok {
			m.fs[dir] = &fstest.MapFile{Mode: fs.ModeDir | 0o755}
		}
	}
}

// WriteFile writes a file into the in-memory filesystem.
func (m *Memory) WriteFile(rel string, body []byte) {
	m.t.Helper()

	name := clean(rel)
	for _, dir := range parentDirs(name) {
		if dir == "." || dir == "" {
			continue
		}

		if _, ok := m.fs[dir]; !ok {
			m.fs[dir] = &fstest.MapFile{Mode: fs.ModeDir | 0o755}
		}
	}

	data := make([]byte, len(body))
	copy(data, body)
	m.fs[name] = &fstest.MapFile{Data: data, Mode: 0o644}
}

// ReadFile reads a file from the in-memory filesystem.
func (m *Memory) ReadFile(rel string) []byte {
	m.t.Helper()

	body, err := fs.ReadFile(m.fs, clean(rel))
	if err != nil {
		m.t.Fatalf("testfs: read %q: %v", rel, err)
	}

	return body
}

func clean(rel string) string {
	name := filepath.ToSlash(rel)
	name = strings.TrimPrefix(name, "./")

	name = strings.TrimPrefix(name, "/")
	if name == "" {
		return "."
	}

	return path.Clean(name)
}

func parentDirs(name string) []string {
	if name == "." || name == "" {
		return nil
	}

	var dirs []string

	dir := path.Dir(name)
	for dir != "." && dir != "/" && dir != "" {
		dirs = append([]string{dir}, dirs...)
		dir = path.Dir(dir)
	}

	return dirs
}
