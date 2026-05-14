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

func TestIsSnapshot_RecognisesSuffix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		given string
		want  bool
	}{
		{"snapshot_with_version", "1.2.3-SNAPSHOT", true},
		{"plain_release_is_not", "1.2.3", false},
		{"snapshot_with_trailing_qualifier_is_not", "0.0.1-SNAPSHOT-rc1", false},
		{"empty_is_not", "", false},
		{"bare_snapshot_marker_is", "-SNAPSHOT", true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, build.IsSnapshot(testCase.given))
		})
	}
}

func TestRenderMavenSummary_SkipTestsTrue_FullMatch(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 34, 56, 0, time.UTC)

	got := build.RenderMavenSummary(build.MavenSummaryInput{
		BuildType:   "lib",
		GroupID:     "se.digg.example",
		ArtifactID:  "demo",
		Version:     "1.2.3-SNAPSHOT",
		JavaVersion: "25",
		SkipTests:   true,
		IsSnapshot:  true,
	}, now)

	wantLines := []string{
		"## Maven Build Summary 🔨",
		"",
		"- **Type:** lib",
		"- **Artifact:** `se.digg.example:demo:1.2.3-SNAPSHOT`",
		"- **Java:** 25",
		"- **Tests:** ⊘ Skipped",
		"- **Snapshot:** true",
		"",
		"*Build completed at 2026-05-10 12:34:56 UTC*",
	}
	require.Equal(t, strings.Join(wantLines, "\n")+"\n", got)
}

func TestRenderMavenSummary_TestsExecuted_Markers(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	got := build.RenderMavenSummary(build.MavenSummaryInput{
		BuildType:   "app",
		GroupID:     "g",
		ArtifactID:  "a",
		Version:     "1.0.0",
		JavaVersion: "21",
		SkipTests:   false,
		IsSnapshot:  false,
	}, now)

	require.Contains(t, got, "- **Tests:** ✓ Executed\n")
	require.Contains(t, got, "- **Snapshot:** false\n")
}
