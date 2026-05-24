// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package publish wires `reusable-ci publish <registry> <subcmd>` for
// publish-side pre-flight validators and output helpers. The actual registry
// upload invocations remain in workflow YAML; the binary owns the brittle
// validation, selection, and machine-readable output glue.
//
// Registry-auth presence validation lives under `validate auth registry`
// (alongside the other release-flow auth checks), not here.
package publish

import "github.com/urfave/cli/v3"

// New returns the `publish` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "publish",
		Usage: "publish-side pre-flight validators and output helpers",
		Commands: []*cli.Command{
			appStoreCmd(),
			googlePlayCmd(),
			mavenCentralCmd(),
			npmCmd(),
		},
	}
}
