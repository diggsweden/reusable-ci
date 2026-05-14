// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/domain/build"
)

func TestRenderNPMSummary_TestsExecuted_FullMatch(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	got := build.RenderNPMSummary(build.NPMSummaryInput{
		PackageName: "@digg/example",
		Version:     "1.2.3",
		NodeVersion: "24",
		SkipTests:   false,
	}, now)

	wantLines := []string{
		"## NPM Build Summary 🔨",
		"",
		"- **Package:** `@digg/example@1.2.3`",
		"- **Node.js:** 24",
		"- **Tests:** ✓ Executed",
		"",
		"*Build completed at 2026-05-10 09:00:00 UTC*",
	}
	require.Equal(t, strings.Join(wantLines, "\n")+"\n", got)
}

func TestRenderNPMSummary_SkipTestsFlipsLine(t *testing.T) {
	t.Parallel()
	got := build.RenderNPMSummary(build.NPMSummaryInput{
		PackageName: "p", Version: "0.1.0", NodeVersion: "20", SkipTests: true,
	}, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC))

	require.Contains(t, got, "- **Tests:** ⊘ Skipped\n")
}
