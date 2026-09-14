// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	appsbom "github.com/diggsweden/reusable-ci/v3/internal/app/sbom"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
)

type guardSigner struct{ calls []string }

func (s *guardSigner) SignFile(_ context.Context, file string) error {
	s.calls = append(s.calls, file)

	return nil
}

func TestSignAssembled_PreflightsAllLinksBeforeSigning(t *testing.T) {
	t.Parallel()

	for _, parent := range []bool{false, true} {
		root := t.TempDir()
		outside := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(outside, "z-sbom.cyclonedx.json"), []byte("canary"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(root, "a-sbom.cyclonedx.json"), []byte("good"), 0o600))

		if parent {
			alias := filepath.Join(t.TempDir(), "alias")
			require.NoError(t, os.Symlink(outside, alias))
			root = alias
		} else {
			require.NoError(t, os.Symlink(filepath.Join(outside, "z-sbom.cyclonedx.json"), filepath.Join(root, "z-sbom.cyclonedx.json")))
		}

		signer := &guardSigner{}

		var log bytes.Buffer

		err := appsbom.SignAssembled(t.Context(), signer, root, &log)
		require.ErrorIs(t, err, errs.ErrValidation)
		require.Empty(t, signer.calls)
		require.Empty(t, log.String())
	}
}
