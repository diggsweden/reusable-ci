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

// prefixedSHA256DigestPattern is the canonical OCI content digest regex — the
// `sha256:` prefix anchored directly against 64 hex characters. The caret sits
// before the prefix, which the composed digest-pinned-ref patterns
// (`…@sha256:[0-9a-f]{64}$`) never do, so this literal is a precise sentinel for
// the standalone digest form alone.
const prefixedSHA256DigestPattern = "^sha256:[0-9a-f]{64}$"

// TestPrefixedSHA256DigestRegexIsSingleSourced keeps the `sha256:<64-hex>`
// digest shape single-sourced in domain/container (container.ValidDigest),
// beside its bare-hex sibling. It is a trust invariant — a mutable tag must
// never pass where a pinned content digest is required — so it lives in one
// place rather than being re-compiled in every package that validates or
// records digest-pinned refs. Callers use container.ValidDigest.
func TestPrefixedSHA256DigestRegexIsSingleSourced(t *testing.T) {
	t.Parallel()

	allowed := map[string]bool{
		"internal/lexiconguard/prefixeddigest_guard_test.go": true,
		"internal/lexiconguard/hexdigest_guard_test.go":      true, // references the literal in a doc comment.
		"internal/domain/container/ref.go":          true,
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

		if strings.Contains(string(content), prefixedSHA256DigestPattern) {
			offenders = append(offenders, filepath.ToSlash(rel))
		}

		return nil
	})
	require.NoError(t, err)

	require.Emptyf(t, offenders,
		"canonical sha256-digest regex re-declared outside domain/container; call container.ValidDigest instead: %v",
		offenders)
}
