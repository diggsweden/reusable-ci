// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package publish wires `reusable-ci publish <registry> <subcmd>` for
// the publish-side pre-flight validators (Maven Central artifacts,
// npm tarballs, registry auth). The actual `mvn deploy` / `npm publish`
// invocations remain in the workflow YAML — the binary owns the
// "is this safe to publish?" gates.
package publish

import "github.com/urfave/cli/v3"

// New returns the `publish` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "publish",
		Usage: "publish-side pre-flight validators (maven-central / npm / registry auth)",
		Commands: []*cli.Command{
			mavenCentralCmd(),
			npmCmd(),
			validateAuthCmd(),
		},
	}
}
