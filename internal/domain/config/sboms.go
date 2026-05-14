// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package config

import (
	"errors"
	"fmt"
	"strings"
)

// ExpandSBOMs translates an `sboms` enum value to the deduped list of
// CISA SBOM layers it represents.
//
// Accepted input:
//   - "all"  → every layer (build, analyzed-artifact, analyzed-container)
//   - "none" → empty
//   - comma-separated list of layer tokens (whitespace tolerated)
//
// "all" and "none" cannot be combined with other tokens.
//
// Mirrors scripts/config/expand-sboms.sh exactly. Returns an error with
// a clear message on invalid input.
func ExpandSBOMs(value string) ([]SBOMLayer, error) {
	cleaned := stripWhitespace(value)
	if cleaned == "" {
		return nil, errors.New("sboms: value required (expected: all | none | comma-list of build,analyzed-artifact,analyzed-container)")
	}
	if strings.HasPrefix(cleaned, ",") || strings.HasSuffix(cleaned, ",") || strings.Contains(cleaned, ",,") {
		return nil, fmt.Errorf("sboms: empty token in %q (check for leading, trailing or duplicate commas)", value)
	}

	switch cleaned {
	case "all":
		// Return a copy so callers can't mutate ValidSBOMLayers.
		out := make([]SBOMLayer, len(ValidSBOMLayers))
		copy(out, ValidSBOMLayers)
		return out, nil
	case "none":
		return []SBOMLayer{}, nil
	}

	parts := strings.Split(cleaned, ",")
	seen := make(map[SBOMLayer]struct{}, len(parts))
	out := make([]SBOMLayer, 0, len(parts))
	for _, p := range parts {
		switch SBOMLayer(p) {
		case SBOMLayerBuild, SBOMLayerAnalyzedArtifact, SBOMLayerAnalyzedContainer:
			layer := SBOMLayer(p)
			if _, dup := seen[layer]; dup {
				continue
			}
			seen[layer] = struct{}{}
			out = append(out, layer)
		case "all", "none":
			return nil, fmt.Errorf("sboms: %q is a shortcut and cannot be combined with other values", p)
		default:
			return nil, fmt.Errorf("sboms: unknown token %q (valid: build, analyzed-artifact, analyzed-container, or shortcuts all|none)", p)
		}
	}
	return out, nil
}

// PipelineSBOMs is the union of every artefact's effective layers, returned
// in canonical order (build, analyzed-artifact, analyzed-container). Used to
// emit the pipeline-sboms output consumed by stage workflows.
//
// Returns "none" when the union is empty (matches the bash literal).
func PipelineSBOMs(artifacts []Artifact) string {
	seen := make(map[SBOMLayer]struct{})
	for _, a := range artifacts {
		for _, l := range a.EffectiveSBOMs {
			seen[l] = struct{}{}
		}
	}
	parts := make([]string, 0, len(ValidSBOMLayers))
	for _, l := range ValidSBOMLayers {
		if _, ok := seen[l]; ok {
			parts = append(parts, string(l))
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ",")
}

func stripWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := range len(s) {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
