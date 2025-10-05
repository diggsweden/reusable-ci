// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cliio_test

import (
	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestRootReadBoundary_PreservesConfinementAndSizeLimit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "root"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "external"), []byte("canary"), 0o600))
	root, err := os.OpenRoot(filepath.Join(dir, "root"))
	require.NoError(t, err)

	defer func() { _ = root.Close() }()

	require.NoError(t, root.Symlink("../external", "alias"))
	body, err := cliio.ReadFileInRoot(root, "alias")
	require.Error(t, err)
	require.Empty(t, body)
	require.NoError(t, root.WriteFile("input", []byte("owned"), 0o600))
	body, err = cliio.ReadFileInRoot(root, "input")
	require.NoError(t, err)
	require.Equal(t, "owned", string(body))

	file, err := root.Create("large")
	require.NoError(t, err)
	require.NoError(t, file.Truncate((64<<20)+1))
	require.NoError(t, file.Close())

	body, err = cliio.ReadFileInRoot(root, "large")
	require.ErrorIs(t, err, errs.ErrMalformedInput)
	require.Empty(t, body)
}
