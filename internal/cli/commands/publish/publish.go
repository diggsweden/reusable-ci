// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package publish wires `reusable-ci publish <registry> <subcmd>` for
// publish-side validators, output helpers, and registry uploads.
//
// Most registry uploads remain in workflow YAML (the binary owns the brittle
// validation, selection, and machine-readable output glue). The exception is
// `forge-packages deploy`: the forge-native Maven registry differs by forge in
// both URL and auth, so the binary owns that whole sequence (resolve → write
// settings.xml → mvn deploy) and each forge component is a thin one-liner.
//
// Registry-auth presence validation lives under `validate auth registry`
// (alongside the other release-flow auth checks), not here.
package publish

import "github.com/urfave/cli/v3"

// New returns the `publish` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "publish",
		Usage: "publish-side validators, output helpers, and registry uploads",
		Commands: []*cli.Command{
			appStoreCmd(),
			forgePackagesCmd(),
			gradleCmd(),
			googlePlayCmd(),
			mavenCentralCmd(),
			npmCmd(),
		},
	}
}
