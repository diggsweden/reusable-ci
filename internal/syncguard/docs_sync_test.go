// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package syncguard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/cli"
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

	path := filepath.Join(reporoot.Path(t), "docs", "cli-reference.md")
	got, err := os.ReadFile(path) //nolint:gosec // test reads repo-local docs file.
	require.NoErrorf(t, err, "read %s", path)

	require.Equalf(t, want, string(got),
		"docs/cli-reference.md is out of sync with the CLI surface; "+
			"run `just gen-cli-reference` to refresh")
}
