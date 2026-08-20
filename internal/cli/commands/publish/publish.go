// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

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
			gradleCmd(),
			mavenCentralCmd(),
			npmCmd(),
		},
	}
}
