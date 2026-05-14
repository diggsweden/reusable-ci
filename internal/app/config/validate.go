// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package config wires `reusable-ci config <subcmd>` to the domain
// config logic. Each subcommand is one file.
package config

import (
	"fmt"
	"io"
	"os"

	"github.com/diggsweden/reusable-ci/internal/domain/config"
)

// Validate loads `path`, parses + validates it, and emits any non-fatal
// warnings to `warnW`. Returns a *config.ValidationError when the schema
// is violated; nil when the config is clean.
func Validate(path string, warnW io.Writer) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config: read %q: %w", path, err)
	}
	c, err := config.Parse(data)
	if err != nil {
		return err
	}
	if err := config.Validate(c); err != nil {
		return err
	}
	for _, w := range config.Warnings(c) {
		fmt.Fprintf(warnW, "warning: %s\n", w)
	}
	return nil
}
