// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// ValidProjectTypes is the SBOM-context valid list — accepted by the
// `sbom generate --project-type` flag, in argv order from the bash for
// consistent diagnostics. Includes Auto (request detection) and
// excludes Meta (not a buildable type for SBOM purposes).
//
//nolint:gochecknoglobals // schema enumeration — read-only and ordered.
var ValidProjectTypes = []projecttype.Type{
	projecttype.Auto, projecttype.Maven, projecttype.NPM, projecttype.Gradle,
	projecttype.GradleAndroid, projecttype.XcodeIOS, projecttype.Python,
	projecttype.Go, projecttype.Cargo,
}

// IsValidProjectType reports whether s is in ValidProjectTypes.
func IsValidProjectType(s string) bool {
	return projecttype.IsIn(projecttype.Type(s), ValidProjectTypes)
}

// DetectProjectType is preserved as a thin wrapper over
// projecttype.DetectFromEntries for callers that already import this
// package.
func DetectProjectType(dirEntries []string) projecttype.Type {
	return projecttype.DetectFromEntries(dirEntries)
}

// ParseLayerCSV splits a comma-separated layer list and trims
// whitespace from each entry. Empty entries are dropped.
//
// Recognised layers: "build", "analyzed-artifact", "analyzed-container".
// Unknown layers are returned in the slice as-is — callers decide whether
// to error or skip.
func ParseLayerCSV(s string) []string {
	parts := strings.Split(s, ",")

	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t == "" {
			continue
		}

		out = append(out, t)
	}

	return out
}

// LayerName is one of the three canonical layer identifiers.
type LayerName string

// Recognised LayerName values.
const (
	LayerBuild             LayerName = "build"
	LayerAnalyzedArtifact  LayerName = "analyzed-artifact"
	LayerAnalyzedContainer LayerName = "analyzed-container"
)

// IsValidLayer reports whether s names one of the three layers.
func IsValidLayer(s string) bool {
	switch LayerName(s) {
	case LayerBuild, LayerAnalyzedArtifact, LayerAnalyzedContainer:
		return true
	}

	return false
}

// UnknownLayerError is returned by callers that want to surface an
// invalid layer name with a consistent message.
func UnknownLayerError(layer string) error {
	return fmt.Errorf("unknown layer: %s (valid: build, analyzed-artifact, analyzed-container): %w", layer, errs.ErrValidation)
}
