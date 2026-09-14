// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestTagGrammarBoundary_ApplyUsesCanonicalValidator(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		tag   string
		valid bool
	}{{"a", true}, {"A_Z.9-x", true}, {"_", true}, {strings.Repeat("a", 128), true}, {strings.Repeat("a", 129), false}, {".a", false}, {"-a", false}, {"a/b", false}, {"a:b", false}, {"a+z", false}, {"a\n", false}, {"a\u00e9", false}} {
		require.Equal(t, tc.valid, container.ValidOCITagComponent(tc.tag), tc.tag)

		got, ok, err := container.Apply(container.Rule{Type: container.RuleTypeRaw, Value: tc.tag, Enable: true}, container.MetadataContext{})
		if tc.valid {
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, tc.tag, got.Tag)
		} else {
			require.Error(t, err)
			require.False(t, ok)
		}
	}

	require.False(t, container.ValidOCITagComponent(""))
}
