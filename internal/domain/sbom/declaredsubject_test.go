// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/sbom"
	"github.com/stretchr/testify/require"
)

func TestReadDeclaredSubject_WhatTheDocumentClaims(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		body string
		want sbom.DeclaredSubject
		err  error
	}{
		{
			name: "a component names one released thing",
			body: `{"bomFormat":"CycloneDX","specVersion":"1.5","metadata":{"component":{"name":"lib","version":"1.2.3"}}}`,
			want: sbom.DeclaredSubject{Name: "lib", Version: "1.2.3"},
		},
		{
			name: "no metadata is an aggregate",
			body: `{"bomFormat":"CycloneDX","components":[{"name":"dep"}]}`,
			want: sbom.DeclaredSubject{Aggregate: true},
		},
		{
			name: "metadata without a component is an aggregate",
			body: `{"bomFormat":"CycloneDX","metadata":{"timestamp":"2026-01-01T00:00:00Z"}}`,
			want: sbom.DeclaredSubject{Aggregate: true},
		},
		{
			name: "a component may omit its version",
			body: `{"bomFormat":"cyclonedx","metadata":{"component":{"name":"lib"}}}`,
			want: sbom.DeclaredSubject{Name: "lib"},
		},
		{name: "another format is not a CycloneDX build layer", body: `{"spdxVersion":"SPDX-2.3"}`, err: errs.ErrMalformedInput},
		{name: "no format at all", body: `{}`, err: errs.ErrMalformedInput},
		{name: "not JSON", body: "not a document", err: errs.ErrMalformedInput},
		{name: "empty", body: "", err: errs.ErrMalformedInput},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := sbom.ReadDeclaredSubject([]byte(testCase.body))
			if testCase.err != nil {
				require.ErrorIs(t, err, testCase.err)
				require.Equal(t, sbom.DeclaredSubject{}, got)

				return
			}

			require.NoError(t, err)
			require.Equal(t, testCase.want, got)
		})
	}
}

// TestSameRelease_ComparesTheReleaseCore pins the rule the build-layer binding
// rests on. The same release legitimately reaches a BOM as 3.0.0, v3.0.0,
// 3.0.0-SNAPSHOT or 3.0.0-rc.1 depending on which tool wrote it, so only the
// core is compared; and an unknown quantity is not a mismatch, because refusing
// a release over a version nobody stated would be a guess, not a check.
func TestSameRelease_ComparesTheReleaseCore(t *testing.T) {
	t.Parallel()

	for _, pair := range [][2]string{
		{"1.2.3", "1.2.3"}, {"v1.2.3", "1.2.3"}, {"1.2.3-SNAPSHOT", "1.2.3"},
		{"1.2.3-rc.1", "1.2.3+build.5"}, {" 1.2.3 ", "1.2.3"},
	} {
		same, stated := sbom.SameRelease(pair[0], pair[1])
		require.True(t, stated, pair)
		require.True(t, same, pair)
	}

	for _, pair := range [][2]string{
		{"1.2.3", "1.2.4"}, {"1.2.3", "2.0.0"}, {"0.1.0", "1.0.0"},
	} {
		same, stated := sbom.SameRelease(pair[0], pair[1])
		require.True(t, stated, pair)
		require.False(t, same, pair)
	}

	for _, pair := range [][2]string{
		{"", "1.2.3"}, {"1.2.3", ""}, {"unknown", "1.2.3"}, {"1.2", "1.2.3"},
		{"1.2.3.4", "1.2.3"}, {"abc", "def"}, {"1.2.x", "1.2.3"},
	} {
		_, stated := sbom.SameRelease(pair[0], pair[1])
		require.False(t, stated, pair)
	}
}
