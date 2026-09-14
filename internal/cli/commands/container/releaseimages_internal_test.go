// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestReleaseImagesGroupExposesBoundaryCommands(t *testing.T) {
	t.Parallel()

	commands := releaseImagesGroup().Commands

	found := make([]string, 0, len(commands))
	for _, cmd := range commands {
		found = append(found, cmd.Name)
		require.Empty(t, cmd.Aliases)
	}

	require.ElementsMatch(t, []string{"sign", "validate", "promote", "rollback", "cleanup"}, found)
}
