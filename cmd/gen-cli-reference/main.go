// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Command gen-cli-reference walks the reusable-ci urfave/cli command
// tree and prints a Markdown reference document to stdout. It is the
// generator for docs/cli-reference.md.
//
// Usage:
//
//	go run ./cmd/gen-cli-reference > docs/cli-reference.md
//
// Re-run after every CLI surface change. The result is byte-stable, so
// `git diff` highlights only the genuine surface changes.
package main

import (
	"fmt"
	"os"

	"github.com/diggsweden/reusable-ci/v3/internal/cli"
)

func main() {
	body := cli.Render(cli.New(cli.BuildInfo{Version: "dev"}))
	if _, err := fmt.Fprint(os.Stdout, body); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Error:", err)

		os.Exit(1)
	}
}
