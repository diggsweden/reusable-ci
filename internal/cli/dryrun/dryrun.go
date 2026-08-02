// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package dryrun holds the single spelling of the shared --dry-run flag so
// every mutating verb presents one preview convention: the flag name, the
// "preview X without performing them" usage template, and the reader are
// defined once and reused across command packages.
package dryrun

import "github.com/urfave/cli/v3"

// FlagName is the one spelling of the preview flag across the CLI.
const FlagName = "dry-run"

// Flag returns the shared --dry-run flag. mutations names what the preview
// would skip, e.g. "registry mutations (copies/deletes)".
//
// Long-flag only: the rest of the CLI exposes no short flags, so a lone
// -n would imply a short-flag convention that does not exist elsewhere.
func Flag(mutations string) cli.Flag {
	return &cli.BoolFlag{Name: FlagName, Usage: "preview " + mutations + " without performing them"}
}

// Enabled reports whether the command was invoked with --dry-run.
func Enabled(cmd *cli.Command) bool {
	return cmd.Bool(FlagName)
}
