// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package openpgp_test

import (
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/openpgp"
	"github.com/stretchr/testify/require"
	"testing"
)

// TestMetadata_ReportsThePrimaryIdentity pins which User ID the metadata
// carries: the one the key's owner marked primary, not whichever sorts first.
// git user.name and user.email on release commits are set from it, so the
// lexical choice put a secondary identity on every release when its name
// sorted earlier. Repeated reads pin the choice as stable.
func TestMetadata_ReportsThePrimaryIdentity(t *testing.T) {
	entity := mintTestEntity(t)
	require.NoError(t, entity.AddUserId("Alpha", "", "alpha@example.invalid", nil))

	primary := entity.PrimaryIdentity()
	require.Equal(t, "Test", primary.UserId.Name, "fixture: the minted identity stays primary")

	body := armorPublicKey(t, entity)
	for range 32 {
		metadata, err := openpgp.ReadMetadata(body)
		require.NoError(t, err)
		require.Equal(t, "Test", metadata.Name)
		require.Equal(t, "test@example.com", metadata.Email)
	}
}
