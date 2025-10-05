// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Command gen-release-images-schema renders the release-image ledger JSON Schema
// from the Go schema declarations in internal/domain/imageledger and prints
// it to stdout. It is the generator for docs/schemas/release-images.schema.json.
//
// Usage:
//
//	go run ./cmd/gen-release-images-schema > docs/schemas/release-images.schema.json
//
// Re-run after every ledger contract change (field, pattern, or
// image-kind change). The result is byte-stable,
// so `git diff` highlights only the genuine schema changes.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
)

func main() {
	body := imageledger.RenderReleaseImagesJSONSchema()

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
