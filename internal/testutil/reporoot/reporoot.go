// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package reporoot resolves the repository root for tests that assert on
// files rather than on behaviour.
//
// The repo-wide guards (internal/archguard, internal/lexiconguard,
// internal/syncguard, internal/workflowguard) all walk the tree from its
// root, and every one of them needs the same answer. Deriving it once here
// keeps them from each carrying a copy -- which would be a poor look in a
// codebase that guards against duplicated constants for a living.
package reporoot

import (
	"path/filepath"
	"runtime"
	"testing"
)

// Path returns the absolute path to the repository root.
//
// It resolves from this source file's own location rather than the working
// directory, so the answer does not depend on which directory `go test` was
// invoked from, nor on how deep the calling package sits.
func Path(t *testing.T) string {
	t.Helper()

	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) returned !ok; cannot locate the repository root")
	}

	// here = <root>/internal/testutil/reporoot/reporoot.go
	return filepath.Clean(filepath.Join(filepath.Dir(here), "..", "..", ".."))
}
