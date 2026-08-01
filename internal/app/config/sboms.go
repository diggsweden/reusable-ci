// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
)

// ExpandSBOMsFormat selects the output encoding for ExpandSBOMs.
type ExpandSBOMsFormat string

const (
	// ExpandSBOMsFormatJSON emits a JSON array of layer strings.
	// e.g. ["build","analyzed-artifact"].
	ExpandSBOMsFormatJSON ExpandSBOMsFormat = "json"
	// ExpandSBOMsFormatComma emits the layers as a comma-list.
	// e.g. build,analyzed-artifact (empty when expansion is empty;
	// no "none" literal — same as the bash --format comma path).
	ExpandSBOMsFormatComma ExpandSBOMsFormat = "comma"
)

// ExpandSBOMsInput drives `config expand-sboms`.
type ExpandSBOMsInput struct {
	Value   string
	Format  ExpandSBOMsFormat // default JSON
	Exclude []string          // layers to drop after expansion
	Output  output.Format
	Sink    ci.OutputSink
}

// ExpandSBOMs prints the expanded layer set to out in the requested
// format and, on GitHub/GitLab, also publishes the comma-joined list as
// CI outputs via in.Sink.
//
//nolint:cyclop // expands layer×format×naming permutations for each artifact.
func ExpandSBOMs(ctx context.Context, out io.Writer, in ExpandSBOMsInput) error {
	if in.Value == "" {
		return fmt.Errorf("value required (expected: all | none | comma-list of build,analyzed-artifact,analyzed-container): %w", errs.ErrUsage)
	}

	layers, err := config.ExpandSBOMs(in.Value)
	if err != nil {
		return err
	}

	excludeSet := make(map[string]bool, len(in.Exclude))
	for _, ex := range in.Exclude {
		excludeSet[strings.TrimSpace(ex)] = true
	}

	keep := layers[:0]
	for _, l := range layers {
		if !excludeSet[string(l)] {
			keep = append(keep, l)
		}
	}

	format := in.Format
	if format == "" {
		format = ExpandSBOMsFormatJSON
	}

	comma := strings.Join(layerStrings(keep), ",")
	if in.Sink != nil && (in.Output == output.FormatGitHub || in.Output == output.FormatGitLab) {
		if err := in.Sink.Set(ctx, "layers", comma); err != nil {
			return err
		}

		hasLayers := "false"
		if comma != "" {
			hasLayers = "true"
		}

		if err := in.Sink.Set(ctx, "has-layers", hasLayers); err != nil {
			return err
		}
	}

	switch format {
	case ExpandSBOMsFormatJSON:
		strs := layerStrings(keep)
		// json.Marshal returns "null" for nil slices; force "[]" for empty.
		if len(strs) == 0 {
			_, _ = fmt.Fprintln(out, "[]")

			return nil
		}

		b, err := json.Marshal(strs)
		if err != nil {
			return err
		}

		_, _ = fmt.Fprintln(out, string(b))
	case ExpandSBOMsFormatComma:
		_, _ = fmt.Fprintln(out, comma)
	default:
		return fmt.Errorf("--format must be json or comma, got: %s: %w", format, errs.ErrUsage)
	}

	return nil
}

func layerStrings(layers []config.SBOMLayer) []string {
	strs := make([]string, len(layers))
	for i, l := range layers {
		strs[i] = string(l)
	}

	return strs
}
