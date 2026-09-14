// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseImagesReadBoundary_RejectsExternalLedger(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dist := filepath.Join(root, "dist")
	require.NoError(t, os.Mkdir(dist, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "external.json"), []byte("[]"), 0o600))
	ledger := filepath.Join(dist, "ledger.json")
	require.NoError(t, os.Symlink(filepath.Join(root, "external.json"), ledger))
	_, err := releaseImagesLoadLedger(releaseImagesCommon{DistDir: dist, Ledger: ledger, ReleaseTag: "v1.0.0"}, false)
	require.ErrorIs(t, err, errs.ErrValidation)
	require.NoError(t, os.Remove(ledger))
	require.NoError(t, os.WriteFile(ledger, []byte("[]"), 0o600))
	entries, err := releaseImagesLoadLedger(releaseImagesCommon{DistDir: dist, Ledger: ledger, ReleaseTag: "v1.0.0"}, false)
	require.NoError(t, err)
	require.Empty(t, entries)
}
