// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package cmdmeta holds small presentation helpers shared by the command-tree
// packages — currently --help categorization.
package cmdmeta

import "github.com/urfave/cli/v3"

// InCINote prefixes the help Description of commands that read the forge,
// repository, run, and the runner's per-job credentials from the CI job
// environment (the run-artifact store ops). It tells a reader the command is
// not meant to be run standalone at a terminal, so the example below it isn't
// misread as a local invocation. One wording, reused, so it stays consistent.
const InCINote = "Runs inside a CI job: the forge, repository, run and runner token come from\n" +
	"the job environment (there is no --token flag). See `reusable-ci --help`.\n\n"

// WithCategory tags each command with a --help category so a group's `--help`
// renders grouped sections (see the SubcommandHelpTemplate tweak in
// internal/cli/root.go). Command paths are unaffected — this only changes how
// the group's help is laid out. Groups that do not categorize their commands
// keep the flat list via the help template's fallback branch.
func WithCategory(category string, cmds ...*cli.Command) []*cli.Command {
	for _, cmd := range cmds {
		cmd.Category = category
	}

	return cmds
}
