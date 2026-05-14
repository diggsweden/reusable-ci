// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package sbom

import "fmt"

// SBOMFormat is one of the two SBOM document formats syft emits.
type SBOMFormat string

const (
	SBOMFormatSPDX      SBOMFormat = "spdx-json"
	SBOMFormatCycloneDX SBOMFormat = "cyclonedx-json"
)

// SBOMFilename returns the canonical SBOM filename for a layer.
// When customBasename is non-empty, the file is named
// "<customBasename>-sbom.<format-suffix>"; otherwise
// "<name>-<version>-<layer>-sbom.<format-suffix>" is used.
//
// Mirrors the file-naming logic in generate_dual_sboms.
func SBOMFilename(customBasename, name, version, layer string, format SBOMFormat) string {
	suffix := "spdx.json"
	if format == SBOMFormatCycloneDX {
		suffix = "cyclonedx.json"
	}
	if customBasename != "" {
		return fmt.Sprintf("%s-sbom.%s", customBasename, suffix)
	}
	return fmt.Sprintf("%s-%s-%s-sbom.%s", name, version, layer, suffix)
}

// AnalyzedBasename builds the basename for analyzed-* SBOM filenames.
// When sha is non-empty (i.e. inside a git repo), the short SHA is
// injected for traceability: "<file>-<sha>-<layer>". Otherwise:
// "<file>-<layer>" (SHA-less form for test fixtures without git).
//
// Mirrors `_analyzed_basename` in the bash.
func AnalyzedBasename(fileBasename, layer, sha string) string {
	if sha != "" {
		return fmt.Sprintf("%s-%s-%s", fileBasename, sha, layer)
	}
	return fmt.Sprintf("%s-%s", fileBasename, layer)
}

// BuildLayerFilename returns the Build-layer SBOM filename. When sha
// is non-empty the short SHA is injected; otherwise the SHA-less
// fallback is used.
//
// Mirrors `build_layer_filename` in the bash.
func BuildLayerFilename(name, version, sha string) string {
	if sha != "" {
		return fmt.Sprintf("%s-%s-%s-build-sbom.cyclonedx.json", name, version, sha)
	}
	return fmt.Sprintf("%s-%s-build-sbom.cyclonedx.json", name, version)
}
