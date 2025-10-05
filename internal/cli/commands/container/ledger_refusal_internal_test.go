// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLedgerRefusalBoundary_PrecedesDirectoryAndLockCreation(t *testing.T) {
	for _, unsafeSBOM := range []bool{false, true} {
		testenv.New(t)
		root := t.TempDir()

		digest, sbom := "bad-digest", "dist/sbom.json"
		if unsafeSBOM {
			digest = "sha256:" + strings.Repeat("a", 64)
			sbom = "../outside.json"
		}

		command := ledgerAddCmd()
		err := command.Run(t.Context(), []string{command.Name, "--ledger", filepath.Join(root, "absent", "ledger.json"), "--tag", "v1.0.0", "--ref", "registry.invalid/o/app@" + digest, "--digest", digest, "--final-tag", "registry.invalid/o/app:v1.0.0", "--sbom", sbom})
		require.ErrorIs(t, err, errs.ErrValidation)
		entries, err := os.ReadDir(root)
		require.NoError(t, err)
		require.Empty(t, entries)
	}
}
