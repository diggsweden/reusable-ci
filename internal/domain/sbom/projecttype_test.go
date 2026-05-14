// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package sbom_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/internal/domain/sbom"
)

func TestDetectProjectType_DelegatesToProjectTypeDetection(t *testing.T) {
	t.Parallel()
	require.Equal(t, projecttype.Maven, sbom.DetectProjectType([]string{"pom.xml", "package.json"}))
	require.Equal(t, projecttype.Unknown, sbom.DetectProjectType([]string{"README.md"}))
}

func TestIsValidProjectType_KnownTypes(t *testing.T) {
	t.Parallel()
	require.True(t, sbom.IsValidProjectType("auto"))
	require.False(t, sbom.IsValidProjectType("ruby"))
}

func TestParseLayerCSV_TrimsAndDropsEmpty(t *testing.T) {
	t.Parallel()
	got := sbom.ParseLayerCSV("build, analyzed-artifact ,, analyzed-container ")
	require.Equal(t, []string{"build", "analyzed-artifact", "analyzed-container"}, got)
}

func TestIsValidLayer_KnownLayers(t *testing.T) {
	t.Parallel()
	for _, layer := range []string{"build", "analyzed-artifact", "analyzed-container"} {
		require.True(t, sbom.IsValidLayer(layer), "%q should be valid", layer)
	}
	require.False(t, sbom.IsValidLayer("source"))
}

func TestUnknownLayerError(t *testing.T) {
	t.Parallel()

	err := sbom.UnknownLayerError("source")
	require.Contains(t, err.Error(), "Unknown layer: source")
	require.Contains(t, err.Error(), "valid: build, analyzed-artifact, analyzed-container")
}
