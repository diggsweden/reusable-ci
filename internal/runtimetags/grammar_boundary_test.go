// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package runtimetags_test

import (
	"github.com/diggsweden/reusable-ci/v3/internal/runtimetags"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeGrammarBoundary_CompleteTagsAndStableVersions(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"latest", "verify", "v1.2.3+build", "v1.2.3-rc.1", "v01.2.3", "v1.02.3", "v1.2.03", "v1.2", "V1.2.3", "1.2.3", "", " v1.2.3", "v1.2.3 "} {
		require.False(t, runtimetags.ValidVersion(version), version)
	}

	for _, version := range []string{"v0.0.0", "v1.2.3", "v999.999.999"} {
		require.True(t, runtimetags.ValidVersion(version), version)
	}

	for _, tag := range []string{"latest", "verify", "v1.2.3+build", "v1.2.3-rc.1", "v1.2", "v1.2.3suffix", "v1.2.3.4"} {
		input := "image: 'ghcr.io/org/reusable-ci-runtime-base:" + tag + "' # comment\n"
		require.Equal(t, []string{tag}, runtimetags.Tags(input))
		got, count := runtimetags.Rewrite(input, "v2.0.0")
		require.Equal(t, input, got)
		require.Zero(t, count)
	}

	require.Equal(t, []string{"v1.2.3", "verify"}, runtimetags.Tags("reusable-ci-runtime:v1.2.3, (reusable-ci-runtime-base:verify)"))
	require.Empty(t, runtimetags.Tags("prefixreusable-ci-runtime-base:v1.2.3 reusable-ci-runtimeother:v1.2.3"))
}

func TestRuntimeSurfaceBoundary_IncludesTemplatesAndRejectsLinks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, path := range []string{".github/workflows/build.yml", "templates/nested/build.yaml"} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte("fixture"), 0o600))
	}

	files, err := runtimetags.Files(root)
	require.NoError(t, err)
	require.Equal(t, []string{".github/workflows/build.yml", "templates/nested/build.yaml"}, files)
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(root, "templates", "linked")))
	_, err = runtimetags.Files(root)
	require.Error(t, err)
}
