// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
	"net"
	"path/filepath"
	"testing"
)

func TestLedgerMerge_MissingLocalInputExits66(t *testing.T) {
	t.Parallel()
	data, count, err := mergeLedgerDocs([]string{filepath.Join(t.TempDir(), "missing.json")})
	require.ErrorIs(t, err, errs.ErrMissingInput)
	require.EqualValues(t, 66, errs.ExitCodeFromError(err))
	require.Empty(t, data)
	require.Zero(t, count)
	require.EqualValues(t, 69, errs.ExitCodeFromError(&net.DNSError{IsTimeout: true}))
}
