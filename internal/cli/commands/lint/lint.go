// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package lint wires `reusable-ci lint <tool> ...` — code-style lint wrappers
// that emit a step-summary block. Distinct from `build` (these don't build) and
// from `security` (style, not vulnerability scanning).
package lint

import "github.com/urfave/cli/v3"

// New returns the `lint` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "lint",
		Usage: "code-style lint wrappers (swift-format, swiftlint)",
		Commands: []*cli.Command{
			swiftCmd(),
		},
	}
}
