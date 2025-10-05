// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package reporoot

import (
	"fmt"
	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
	"os"
	"path/filepath"
	"testing"
)

// ReadFile reads a repository-relative regular file without accepting linked
// parent directories or leaves. Guard fixtures must not read outside the checkout.
func ReadFile(t *testing.T, relative string) []byte {
	t.Helper()

	body, err := ReadFileAt(Path(t), relative)
	if err != nil {
		t.Fatalf("read repository input %s: %v", relative, err)
	}

	return body
}

// ReadFileAt applies the same policy to an explicitly owned fixture root.
func ReadFileAt(root, relative string) ([]byte, error) {
	if !pathsafe.Relative(relative) || relative == "." {
		return nil, fmt.Errorf("repository input must be relative: %w", errs.ErrValidation)
	}

	parent, err := pathsafe.OpenRoot(filepath.Join(root, filepath.Dir(relative)))
	if err != nil {
		return nil, err
	}

	defer func() { _ = parent.Close() }()

	info, err := parent.Lstat(filepath.Base(relative))
	if err != nil {
		return nil, err
	}

	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("repository input must be regular: %w", errs.ErrValidation)
	}

	return cliio.ReadFileInRoot(parent, filepath.Base(relative))
}

// ReadDir opens a governed repository directory through checked components.
func ReadDir(t *testing.T, relative string) []os.DirEntry {
	t.Helper()

	if !pathsafe.Relative(relative) {
		t.Fatalf("repository directory is not relative: %s", relative)
	}

	root, err := pathsafe.OpenRoot(filepath.Join(Path(t), relative))
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = root.Close() }()

	file, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = file.Close() }()

	entries, err := file.ReadDir(-1)
	if err != nil {
		t.Fatal(err)
	}

	return entries
}
