// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package config

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/config"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
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
}

// ExpandSBOMs prints the expanded layer set to out in the requested
// format. Mirrors scripts/config/expand-sboms.sh CLI behaviour.
func ExpandSBOMs(out io.Writer, in ExpandSBOMsInput) error {
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
	switch format {
	case ExpandSBOMsFormatJSON:
		strs := make([]string, len(keep))
		for i, l := range keep {
			strs[i] = string(l)
		}
		// json.Marshal returns "null" for nil slices; force "[]" for empty.
		if len(strs) == 0 {
			fmt.Fprintln(out, "[]")
			return nil
		}
		b, err := json.Marshal(strs)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, string(b))
	case ExpandSBOMsFormatComma:
		parts := make([]string, len(keep))
		for i, l := range keep {
			parts[i] = string(l)
		}
		fmt.Fprintln(out, strings.Join(parts, ","))
	default:
		return fmt.Errorf("--format must be json or comma, got: %s", format)
	}
	return nil
}
