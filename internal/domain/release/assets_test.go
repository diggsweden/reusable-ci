// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/domain/release"
)

func TestCollectAssets_DedupesByBasename(t *testing.T) {
	t.Parallel()

	got := release.CollectAssets([]string{
		"./release-artifacts/my-app-1.0.0.tgz",
		"my-app-1.0.0.tgz", // duplicate basename — drop
		"my-app-1.0.0.tgz.asc",
		"checksums.sha256", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"checksums.sha256", // duplicate — drop
		"",                 // empty — skip
	})
	want := []string{
		"./release-artifacts/my-app-1.0.0.tgz",
		"my-app-1.0.0.tgz.asc",
		"checksums.sha256",
	}
	require.Equal(t, want, got)
}

func TestSignatureSidecars_IncludesBothGPGAndCosignLayouts(t *testing.T) {
	t.Parallel()
	require.Equal(t,
		[]string{"foo.zip.asc", "foo.zip.bundle"},
		release.SignatureSidecars("foo.zip"),
	)
}
