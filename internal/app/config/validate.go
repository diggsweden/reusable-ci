// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package config wires `reusable-ci config <subcmd>` to the domain
// config logic. Each subcommand is one file.
package config

import (
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
)

// Validate loads `path` (or stdin when path is "-"), parses + validates
// it, and emits any non-fatal warnings to `warnW`. Returns a
// *config.ValidationError when the schema is violated; nil when the
// config is clean. Warnings are best effort: a nil writer discards them
// and a failing writer does not turn a valid config into an error.
func Validate(path string, warnW io.Writer) error {
	if warnW == nil {
		warnW = io.Discard
	}

	data, err := cliio.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config: read %q: %w", path, err)
	}

	c, err := config.Parse(data) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		return err
	}

	if err := config.Validate(c); err != nil {
		return err
	}

	for _, w := range config.Warnings(c) {
		_, _ = fmt.Fprintf(warnW, "warning: %s\n", w)
	}

	return nil
}
