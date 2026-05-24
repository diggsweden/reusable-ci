// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package cli_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/cli"
)

// TestDocsCLIReferenceInSync asserts that docs/cli-reference.md on disk
// matches what cmd/gen-cli-reference would produce right now. This is
// the `go test` form of `just check-cli-reference`; CI invokes both
// `go test` and the just recipe today, but only this test gates PR
// merges through the standard test infrastructure (no extra shell
// runner, no GitHub-Actions-only `::error::` trick).
//
// On failure, run `just gen-cli-reference` to refresh the file.
func TestDocsCLIReferenceInSync(t *testing.T) {
	t.Parallel()

	want := cli.Render(cli.New(cli.BuildInfo{Version: "dev"}))

	path := filepath.Join(repoRoot(t), "docs", "cli-reference.md")
	got, err := os.ReadFile(path) //nolint:gosec // test reads repo-local docs file.
	require.NoErrorf(t, err, "read %s", path)

	require.Equalf(t, want, string(got),
		"docs/cli-reference.md is out of sync with the CLI surface; "+
			"run `just gen-cli-reference` to refresh")
}

// repoRoot resolves the repository root from this source file's own
// path, so the test works regardless of the directory `go test` is
// invoked from. The location of docs/cli-reference.md is fixed
// relative to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()

	_, here, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller(0) returned !ok")
	// here = .../internal/cli/docs_sync_test.go
	// repo root = two dirs up
	return filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
}
