// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package sbom_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/domain/sbom"
)

func TestSBOMFilename_FromNameVersionLayer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		format sbom.SBOMFormat
		want   string
	}{
		{"spdx_format", sbom.SBOMFormatSPDX, "demo-1.2.3-build-sbom.spdx.json"},
		{"cyclonedx_format", sbom.SBOMFormatCycloneDX, "demo-1.2.3-build-sbom.cyclonedx.json"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := sbom.SBOMFilename("", "demo", "1.2.3", "build", testCase.format)
			require.Equal(t, testCase.want, got)
		})
	}
}

func TestSBOMFilename_FromCustomBasename(t *testing.T) {
	t.Parallel()
	got := sbom.SBOMFilename(
		"demo-1.2.3-abc1234-analyzed-jar",
		"ignored", "ignored", "analyzed-jar",
		sbom.SBOMFormatCycloneDX,
	)
	require.Equal(t, "demo-1.2.3-abc1234-analyzed-jar-sbom.cyclonedx.json", got)
}

func TestAnalyzedBasename_WithAndWithoutSHA(t *testing.T) {
	t.Parallel()
	require.Equal(t,
		"demo-1.2.3-abc1234-analyzed-jar",
		sbom.AnalyzedBasename("demo-1.2.3", "analyzed-jar", "abc1234"),
	)
	require.Equal(t,
		"demo-1.2.3-analyzed-jar",
		sbom.AnalyzedBasename("demo-1.2.3", "analyzed-jar", ""),
	)
}

func TestBuildLayerFilename_WithAndWithoutSHA(t *testing.T) {
	t.Parallel()
	require.Equal(t,
		"demo-1.2.3-abc1234-build-sbom.cyclonedx.json",
		sbom.BuildLayerFilename("demo", "1.2.3", "abc1234"),
	)
	require.Equal(t,
		"demo-1.2.3-build-sbom.cyclonedx.json",
		sbom.BuildLayerFilename("demo", "1.2.3", ""),
	)
}
