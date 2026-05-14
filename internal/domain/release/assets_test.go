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
		"checksums.sha256",
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

func TestSignaturePath_AppendsAscSuffix(t *testing.T) {
	t.Parallel()
	require.Equal(t, "foo.zip.asc", release.SignaturePath("foo.zip"))
}
