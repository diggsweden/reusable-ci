// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package config

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// Parse turns a byte slice (artifacts.yml content) into a typed Config.
// It does not validate or compute derived fields — call Validate next,
// then Compute when you need the SBOM defaults / container enrichment.
func Parse(data []byte) (*Config, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("config: empty input: %w", errs.ErrInvalidConfig)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("config: parse yaml: %w: %w", err, errs.ErrInvalidConfig)
	}
	return &c, nil
}
