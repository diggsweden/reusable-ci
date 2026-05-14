// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
)

func TestNPMBuild_RendersAllFields(t *testing.T) {
	t.Parallel()
	sink := &fakeSummarySink{}
	err := appsummary.NPMBuild(context.Background(), sink, appsummary.NPMBuildInput{
		PackageName: "@digg/example",
		Version:     "1.2.3",
		NodeVersion: "24",
		SkipTests:   false,
		Now:         time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	got := sink.buf.String()
	for _, want := range []string{
		"## NPM Build Summary 🔨",
		"- **Package:** `@digg/example@1.2.3`",
		"- **Node.js:** 24",
		"- **Tests:** ✓ Executed",
		"*Build completed at 2026-05-10 09:00:00 UTC*",
	} {
		require.Contains(t, got, want)
	}
}
