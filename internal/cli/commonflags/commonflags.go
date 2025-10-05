// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package commonflags centralizes CLI flags that recur verbatim across many
// command groups, so a shared flag's name and env-var source live in one place
// instead of as repeated string literals (each formerly carrying a goconst
// waiver). Only the usage text — which is legitimately per-command — is a
// parameter; the drift-prone name/source/default are fixed here. This mirrors
// regflags/signflags, which already do this for registry and signing flags.
package commonflags

import "github.com/urfave/cli/v3"

// WorkingDir is the shared "--working-dir" flag: the directory a command roots
// its file operations at. Sourced from $WORKING_DIRECTORY and defaulting to the
// current directory; only usage varies per command. Commands whose working
// directory has no "." default (e.g. plan's per-artifact override) construct
// their own flag rather than use this.
func WorkingDir(usage string) *cli.StringFlag {
	return &cli.StringFlag{
		Name:    "working-dir",
		Value:   ".",
		Sources: cli.EnvVars("WORKING_DIRECTORY"),
		Usage:   usage,
	}
}
