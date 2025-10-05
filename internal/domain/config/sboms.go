// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/listval"
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
// Returns an error with a clear message on invalid input.
//
//nolint:cyclop // expands per layer × format permutation.
func ExpandSBOMs(value string) ([]SBOMLayer, error) {
	cleaned := stripWhitespace(value)
	if cleaned == "" {
		return nil, fmt.Errorf("sboms: value required (expected: all | none | comma-list of build,analyzed-artifact,analyzed-container)"+": %w", errs.ErrValidation)
	}

	if strings.HasPrefix(cleaned, ",") || strings.HasSuffix(cleaned, ",") || strings.Contains(cleaned, ",,") {
		return nil, fmt.Errorf("sboms: empty token in %q (check for leading, trailing or duplicate commas): %w", value, errs.ErrValidation)
	}

	switch cleaned {
	case "all": //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		// Return a copy so callers can't mutate ValidSBOMLayers.
		out := make([]SBOMLayer, len(ValidSBOMLayers))
		copy(out, ValidSBOMLayers)

		return out, nil
	case "none": //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		return []SBOMLayer{}, nil
	}

	parts := listval.Tokens(cleaned)
	seen := make(map[SBOMLayer]struct{}, len(parts))

	out := make([]SBOMLayer, 0, len(parts))
	for _, p := range parts { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		switch SBOMLayer(p) {
		case SBOMLayerBuild, SBOMLayerAnalyzedArtifact, SBOMLayerAnalyzedContainer:
			layer := SBOMLayer(p)
			if _, dup := seen[layer]; dup {
				continue
			}

			seen[layer] = struct{}{}
			out = append(out, layer)
		case "all", "none":
			return nil, fmt.Errorf("sboms: %q is a shortcut and cannot be combined with other values: %w", p, errs.ErrValidation)
		default:
			return nil, fmt.Errorf("sboms: unknown token %q (valid: build, analyzed-artifact, analyzed-container, or shortcuts all|none): %w", p, errs.ErrValidation)
		}
	}

	return out, nil
}

// PipelineSBOMs is the union of every artifact's effective layers, returned
// in canonical order (build, analyzed-artifact, analyzed-container). The typed
// config plan carries this value for later release planning.
//
// Returns "none" when the union is empty.
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
	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
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
