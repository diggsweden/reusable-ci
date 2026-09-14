// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package runtimetags_test

import (
	"github.com/diggsweden/reusable-ci/v3/internal/runtimetags"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRuntimeTokenBoundary_DoesNotRewritePartialPins(t *testing.T) {
	t.Parallel()

	for _, ref := range []string{"reusable-ci-runtime-base:v1.2.3+build", "reusable-ci-runtime-base:v1.2.3-rc.1", "not-reusable-ci-runtime-base:v1.2.3", "reusable-ci-runtimeother:v1.2.3", "reusable-ci-runtime-base:v1.2.3@sha256:abc"} {
		got, count := runtimetags.Rewrite("image: \""+ref+"\"", "v4.0.0")
		require.Equal(t, "image: \""+ref+"\"", got)
		require.Zero(t, count)
	}

	require.Equal(t, []string{"v1.2.3+build"}, runtimetags.Tags("ghcr.io/org/reusable-ci-runtime-base:v1.2.3+build"))
	require.Empty(t, runtimetags.Tags("not-reusable-ci-runtime-base:v1.2.3"))

	got, count := runtimetags.Rewrite("(reusable-ci-runtime-base:v1.2.3), reusable-ci-runtime:v2.0.0", "v4.0.0")
	require.Equal(t, "(reusable-ci-runtime-base:v4.0.0), reusable-ci-runtime:v4.0.0", got)
	require.Equal(t, 2, count)
}
