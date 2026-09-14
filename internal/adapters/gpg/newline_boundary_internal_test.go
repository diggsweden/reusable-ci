// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gpg

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGPGNewlineBoundary_RemovesOneSeparator(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ input, want string }{{"body\n\n", "body\n"}, {"body\n", "body"}, {"body", "body"}, {"", ""}} {
		got, err := finishRun("unused", nil, []byte(tc.input), nil)
		require.NoError(t, err)
		require.Equal(t, tc.want, got)
	}
}
