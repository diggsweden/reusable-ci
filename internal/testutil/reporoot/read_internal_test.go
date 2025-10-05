// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package reporoot

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestRepositoryReadBoundary_RejectsEveryLinkComponent(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"leaf", "parent", "root", "regular"} {
		owned := t.TempDir()
		root := filepath.Join(owned, "repo")
		outside := filepath.Join(owned, "outside")
		require.NoError(t, os.Mkdir(outside, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(outside, "input"), []byte("outside-canary"), 0o600))

		if kind == "root" {
			require.NoError(t, os.Symlink(outside, root))
		} else {
			require.NoError(t, os.Mkdir(root, 0o700))
		}

		path := "input"

		switch kind {
		case "leaf":
			require.NoError(t, os.Symlink(filepath.Join(outside, "input"), filepath.Join(root, path)))
		case "parent":
			require.NoError(t, os.Symlink(outside, filepath.Join(root, "link")))

			path = "link/input"
		case "regular":
			require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte("owned"), 0o600))
		}

		body, err := ReadFileAt(root, path)
		if kind == "regular" {
			require.NoError(t, err)
			require.Equal(t, "owned", string(body))
		} else {
			require.Error(t, err)
			require.Empty(t, body)
		}
	}
}
