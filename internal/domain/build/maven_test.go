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
		{"plain_release_is_not", "1.2.3", false}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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
		BuildType:   "app", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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

func TestParsePOM_LiteralFields(t *testing.T) {
	t.Parallel()

	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <groupId>se.digg.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.2.3-SNAPSHOT</version>
</project>`)
	pom, err := build.ParsePOM(body)
	require.NoError(t, err)
	require.Equal(t, "se.digg.example", pom.GroupID)
	require.Equal(t, "demo", pom.ArtifactID)
	require.Equal(t, "1.2.3-SNAPSHOT", pom.Version)
}

func TestParsePOM_InheritsGroupAndVersionFromParent(t *testing.T) {
	t.Parallel()

	body := []byte(`<?xml version="1.0"?>
<project>
  <parent>
    <groupId>se.digg.platform</groupId>
    <artifactId>platform-bom</artifactId>
    <version>2.0.0</version>
  </parent>
  <artifactId>child-module</artifactId>
</project>`)
	pom, err := build.ParsePOM(body)
	require.NoError(t, err)
	require.Equal(t, "se.digg.platform", pom.GroupID)
	require.Equal(t, "2.0.0", pom.Version)
	require.Equal(t, "child-module", pom.ArtifactID)
}

func TestParsePOM_DoesNotResolveProperties(t *testing.T) {
	t.Parallel()

	body := []byte(`<?xml version="1.0"?>
<project>
  <groupId>g</groupId>
  <artifactId>a</artifactId>
  <version>${revision}</version>
</project>`)
	pom, err := build.ParsePOM(body)
	require.NoError(t, err)
	require.Equal(t, "${revision}", pom.Version)
	require.True(t, build.POMHasUnresolvedProperty(pom.Version))
	require.False(t, build.POMHasUnresolvedProperty(pom.GroupID))
}

func TestParsePOM_RejectsMalformedXML(t *testing.T) {
	t.Parallel()

	_, err := build.ParsePOM([]byte(`<project><unclosed>`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse pom.xml")
}
