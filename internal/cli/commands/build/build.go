// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package build wires `reusable-ci build <toolchain> <subcmd>` using
// urfave/cli v3. Each toolchain (maven, gradle, gradle-android,
// xcode-ios) is its own subgroup.
package build

import "github.com/urfave/cli/v3"

// New returns the `build` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "build",
		Usage: "toolchain build wrappers (maven, gradle, gradle-android, xcode-ios)",
		Commands: []*cli.Command{
			mavenCmd(),
			gradleCmd(),
			gradleAndroidCmd(),
			xcodeIOSCmd(),
		},
	}
}
