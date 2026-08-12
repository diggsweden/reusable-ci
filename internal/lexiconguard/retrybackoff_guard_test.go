// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package lexiconguard

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"

	"github.com/stretchr/testify/require"
)

// linearBackoffExpr is the attempt-scaled wait math (attempt×delay). Before
// consolidation this exact expression had been hand-rolled into the container
// scan, base-image evidence, and container-push retry loops; each also
// re-implemented the ctx-cancellation select. It now lives once in
// internal/retry (retry.Do's backoffWait).
const linearBackoffExpr = "time.Duration(attempt"

// TestRetryBackoffIsSingleSourced keeps the linear-backoff retry math
// single-sourced in internal/retry, the way the commit-SHA and digest checks
// are single-sourced in their domain packages. App and adapter code retries
// through retry.Do/Run (with retry.WithBackoff for constant delay and
// retry.Permanent for non-retriable errors) rather than re-rolling the
// attempt loop and its wait.
func TestRetryBackoffIsSingleSourced(t *testing.T) {
	t.Parallel()

	allowed := map[string]bool{
		"internal/lexiconguard/retrybackoff_guard_test.go": true,
		"internal/retry/retry.go":                 true,
	}

	root := reporoot.Path(t)

	var offenders []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			if name := entry.Name(); name == ".git" || name == "dist" || name == "node_modules" {
				return filepath.SkipDir
			}

			return nil
		}

		if filepath.Ext(entry.Name()) != ".go" {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		if allowed[filepath.ToSlash(rel)] {
			return nil
		}

		content, readErr := os.ReadFile(path) //nolint:gosec // test walks repo-local files.
		if readErr != nil {
			return readErr
		}

		if strings.Contains(string(content), linearBackoffExpr) {
			offenders = append(offenders, filepath.ToSlash(rel))
		}

		return nil
	})
	require.NoError(t, err)

	require.Emptyf(t, offenders,
		"linear-backoff retry math re-declared outside internal/retry; use retry.Do/Run instead: %v",
		offenders)
}
