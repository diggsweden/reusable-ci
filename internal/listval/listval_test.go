// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package listval_test

import (
	"reflect"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/listval"
)

func TestTokens(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"comma", "a,b,c", []string{"a", "b", "c"}},
		{"spaces", "a b c", []string{"a", "b", "c"}},
		{"newlines", "a\nb\nc", []string{"a", "b", "c"}},
		{"mixed + padding", " a, b\n c ,,d ", []string{"a", "b", "c", "d"}},
		{"empty", "", nil},
		{"only delimiters", " , \n ", nil},
		{"preserves order + dupes", "b,a,b", []string{"b", "a", "b"}},
		{"realistic platforms", "linux/amd64, linux/arm64", []string{"linux/amd64", "linux/arm64"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := listval.Tokens(tc.in)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Tokens(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}
