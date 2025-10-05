// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git_test

import (
	adaptergit "github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTagKeyring_MalformedIsHardErrorAndEmptyIsMiss(t *testing.T) {
	t.Parallel()

	repo := &adaptergit.Repo{Dir: t.TempDir()}
	for _, keyring := range []string{"", " ", "not a key", "-----BEGIN PGP PUBLIC KEY BLOCK-----\ninvalid\n-----END PGP PUBLIC KEY BLOCK-----"} {
		signer, fingerprint, ok, err := repo.VerifyTagSignature(t.Context(), "v1", []byte(keyring))
		if keyring == "" {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, errs.ErrMalformedInput)
		}

		require.False(t, ok)
		require.Empty(t, signer)
		require.Empty(t, fingerprint)
	}
}
