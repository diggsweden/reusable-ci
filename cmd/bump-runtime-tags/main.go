// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Command bump-runtime-tags rewrites every pinned
// reusable-ci-runtime-*:vX.Y.Z reference under .github/workflows/ to a
// new version. It is the release-cut companion of the
// TestRuntimeImageTagsShareOneVersion guard: the guard proves all pins
// agree; this tool is how they are moved together.
//
// Usage:
//
//	go run ./cmd/bump-runtime-tags v3.1.0
//	# or: just bump-runtime-tags v3.1.0
//
// Local-only tags (":verify") are never rewritten. The tool fails if
// no pinned reference is found — an empty rewrite means the layout or
// pattern changed and the pin set silently escaped it.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/runtimetags"
)

var (
	errUsage      = errors.New("usage: bump-runtime-tags <vX.Y.Z>")
	errBadVersion = errors.New("not a release version (vX.Y.Z)")
	errNoRefs     = errors.New("no pinned reusable-ci-runtime references found")
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return errUsage
	}

	version := os.Args[1]
	if !runtimetags.VersionPattern.MatchString(version) {
		return fmt.Errorf("version %q: %w", version, errBadVersion)
	}

	workflowsDir := filepath.Join(".github", "workflows")

	entries, err := os.ReadDir(workflowsDir)
	if err != nil {
		return fmt.Errorf("read %s (run from the repo root): %w", workflowsDir, err)
	}

	total := 0

	for _, entry := range entries {
		count, rewriteErr := rewriteFile(workflowsDir, entry, version)
		if rewriteErr != nil {
			return rewriteErr
		}

		total += count
	}

	if total == 0 {
		return fmt.Errorf("%w under %s", errNoRefs, workflowsDir)
	}

	fmt.Printf("Rewrote %d runtime-image reference(s) to %s\n", total, version)

	return nil
}

// rewriteFile rewrites the pinned runtime-image references in one
// workflow file, reporting how many it moved. Non-.yml entries are
// skipped; untouched files are not rewritten.
func rewriteFile(dir string, entry os.DirEntry, version string) (int, error) {
	if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
		return 0, nil
	}

	path := filepath.Join(dir, entry.Name())

	content, err := os.ReadFile(path) //nolint:gosec // repo-local workflow files from a fixed dir.
	if err != nil {
		return 0, err
	}

	rewritten, count := runtimetags.Rewrite(string(content), version)
	if count == 0 {
		return 0, nil
	}

	info, err := entry.Info()
	if err != nil {
		return 0, err
	}

	if err := os.WriteFile(path, []byte(rewritten), info.Mode()); err != nil { //nolint:gosec // repo-local workflow files from a fixed dir.
		return 0, err
	}

	fmt.Printf("%s: %d reference(s) → %s\n", path, count, version)

	return count, nil
}
