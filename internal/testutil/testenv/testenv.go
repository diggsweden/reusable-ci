// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package testenv provides the canonical isolated environment wrapper for
// env-sensitive tests. It composes isolatedenv and gives tests a small,
// explicit handle for environment mutation and temp paths.
package testenv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/isolatedenv"
)

// Env is the isolated environment handle for a test.
type Env struct {
	t    testing.TB
	Home string
	Temp string
}

// New isolates the process environment for the test and returns a small handle
// for derived paths and additional env variables.
func New(t *testing.T) *Env {
	t.Helper()
	home := isolatedenv.Isolate(t)

	tmp := filepath.Join(home, "tmp")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		t.Fatalf("testenv: mkdir temp dir %q: %v", tmp, err)
	}

	return &Env{t: t, Home: home, Temp: tmp}
}

// Setenv forwards to testing.T.Setenv inside the already-isolated test env.
func (e *Env) Setenv(key, value string) {
	e.t.Helper()
	e.t.Setenv(key, value)
}

// Path returns a path under the isolated temp directory. An absolute or
// climbing name fails the test rather than reaching outside it.
func (e *Env) Path(parts ...string) string {
	e.t.Helper()

	if len(parts) == 0 {
		return e.Temp
	}

	rel := filepath.Join(parts...)
	if !filepath.IsLocal(rel) {
		e.t.Fatalf("testenv: %q is not a local path below the isolated temp directory", rel)
	}

	return filepath.Join(e.Temp, rel)
}

// MkdirAll creates a directory under the isolated temp directory and returns it.
func (e *Env) MkdirAll(parts ...string) string {
	e.t.Helper()

	p := e.Path(parts...)
	if err := os.MkdirAll(p, 0o700); err != nil {
		e.t.Fatalf("testenv: mkdir %q: %v", p, err)
	}

	return p
}
