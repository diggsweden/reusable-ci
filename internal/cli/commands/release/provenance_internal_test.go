// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"slices"
	"testing"
)

func TestProvenanceSignAllow(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		keyRef string
		want   []string
	}{
		{"env_key_isolated", "env://COSIGN_KEY", []string{"COSIGN_KEY", "COSIGN_PASSWORD"}},
		{"env_key_custom_var", "env://MY_SIGNING_KEY", []string{"MY_SIGNING_KEY", "COSIGN_PASSWORD"}},
		{"kms_not_isolated", "hashivault://transit/keys/release", nil},
		{"file_not_isolated", "/keys/cosign.key", nil},
		{"empty_env_prefix_not_isolated", "env://", nil},
		{"empty", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := provenanceSignAllow(tc.keyRef); !slices.Equal(got, tc.want) {
				t.Errorf("provenanceSignAllow(%q) = %v, want %v", tc.keyRef, got, tc.want)
			}
		})
	}
}
