// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package security wires `reusable-ci security <subgroup> <subcmd>`.
//
// Two subgroups by responsibility:
//
//   - scan   — invoke a scanner against the workspace or an image
//     (opengrep, dependencies, container)
//   - report — transform / upload an existing security report (enrich
//     SARIF for GitHub Code Scanning, convert Trivy JSON to the
//     GitLab security-report schemas, upload SARIF)
package security

import "github.com/urfave/cli/v3"

// New returns the `security` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "security",
		Usage: "security scanners and report converters",
		Commands: []*cli.Command{
			scanGroup(),
			reportGroup(),
		},
	}
}
