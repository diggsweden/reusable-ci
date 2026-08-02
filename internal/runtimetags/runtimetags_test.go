// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package runtimetags_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/runtimetags"
)

func TestRewrite(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		in        string
		want      string
		wantCount int
	}{
		{
			name:      "single pinned default",
			in:        `default: "ghcr.io/diggsweden/reusable-ci-runtime-java-25:v3.0.0"`,
			want:      `default: "ghcr.io/diggsweden/reusable-ci-runtime-java-25:v3.1.0"`,
			wantCount: 1,
		},
		{
			name:      "bare runtime image without flavor suffix",
			in:        `default: "ghcr.io/diggsweden/reusable-ci-runtime:v3.0.0"`,
			want:      `default: "ghcr.io/diggsweden/reusable-ci-runtime:v3.1.0"`,
			wantCount: 1,
		},
		{
			name:      "verify tag untouched",
			in:        `local-tag: reusable-ci-runtime-base:verify`,
			want:      `local-tag: reusable-ci-runtime-base:verify`,
			wantCount: 0,
		},
		{
			name: "mixed content rewrites only pins",
			in: `a: "reusable-ci-runtime-base:v3.0.0"
b: reusable-ci-runtime-node-24:verify
c: "reusable-ci-runtime-rust-stable:v3.0.0"`,
			want: `a: "reusable-ci-runtime-base:v3.1.0"
b: reusable-ci-runtime-node-24:verify
c: "reusable-ci-runtime-rust-stable:v3.1.0"`,
			wantCount: 2,
		},
		{
			name:      "unrelated image untouched",
			in:        `image: ghcr.io/other/tool:v3.0.0`,
			want:      `image: ghcr.io/other/tool:v3.0.0`,
			wantCount: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, count := runtimetags.Rewrite(tc.in, "v3.1.0")
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.wantCount, count)
		})
	}
}

func TestVersionPattern(t *testing.T) {
	t.Parallel()

	require.Regexp(t, runtimetags.VersionPattern, "v3.1.0")
	require.NotRegexp(t, runtimetags.VersionPattern, "3.1.0")
	require.NotRegexp(t, runtimetags.VersionPattern, "v3.1.0-rc.1")
	require.NotRegexp(t, runtimetags.VersionPattern, "verify")
}
