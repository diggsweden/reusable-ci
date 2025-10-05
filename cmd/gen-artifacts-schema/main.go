// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Command gen-artifacts-schema renders the artifacts.yml JSON Schema
// from the Go schema declarations in internal/domain/config and prints
// it to stdout. It is the generator for .reusable-ci/artifacts.schema.json.
//
// Usage:
//
//	go run ./cmd/gen-artifacts-schema > .reusable-ci/artifacts.schema.json
//
// Re-run after every schema change (new project type, publish target,
// sign method, SBOM layer, or KMS scheme). The result is byte-stable,
// so `git diff` highlights only the genuine schema changes.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
)

func main() {
	body := config.RenderArtifactsJSONSchema()

	// A leftover placeholder or invalid JSON means the template and
	// the renderer drifted apart — refuse to emit a broken schema.
	if strings.Contains(body, "{{") {
		fmt.Fprintln(os.Stderr, "Error: rendered schema still contains a {{placeholder}}")
		os.Exit(1)
	}

	if !json.Valid([]byte(body)) {
		fmt.Fprintln(os.Stderr, "Error: rendered schema is not valid JSON")
		os.Exit(1)
	}

	if _, err := fmt.Fprint(os.Stdout, body); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
