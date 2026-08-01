// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package build wires `reusable-ci build <toolchain> <subcmd>` using
// urfave/cli v3. Each toolchain (go, cargo, maven, npm, gradle,
// gradle-android, xcode-ios, swift) is its own subgroup.
package build

import "github.com/urfave/cli/v3"

// New returns the `build` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "build",
		Usage: "toolchain build wrappers (go, cargo, maven, npm, gradle, gradle-android, xcode-ios)",
		Commands: []*cli.Command{
			goCmd(),
			cargoCmd(),
			mavenCmd(),
			npmCmd(),
			gradleCmd(),
			gradleAndroidCmd(),
			xcodeIOSCmd(),
		},
	}
}
