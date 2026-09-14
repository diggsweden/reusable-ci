// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package scripts

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestShellDiscoveryBoundary_ExplicitSurfaceAndLinks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for name, body := range map[string]string{"one.sh": "true\n", "two.bash": "true\n", "three.sh.tmpl": "true\n", "command": "#!/bin/sh\ntrue\n", "README": "not a script"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o600))
	}

	var found []string

	count, err := walkShellSources(root, func(path string, _ []byte) error {
		found = append(found, path)

		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 4, count)
	require.ElementsMatch(t, []string{"one.sh", "two.bash", "three.sh.tmpl", "command"}, found)
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(root, "linked")))
	_, err = walkShellSources(root, func(path string, _ []byte) error {
		require.NotEqual(t, "linked", path)

		return nil
	})
	require.Error(t, err)
}
