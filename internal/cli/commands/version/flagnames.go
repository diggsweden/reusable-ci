// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version

// Shared flag names and defaults for the version package. Each name that
// repeats across command definitions is declared once here, so spellings
// cannot drift between verbs and the package stays under goconst's literal
// budget. Values are part of the CLI contract — docs/cli-reference.md is
// generated from them and a sync test gates any change.
const (
	flagTag    = "tag"
	flagBranch = "branch"
	flagToken  = "token"
	// defaultCommitMessageFile is the shared default for --commit-message-file.
	defaultCommitMessageFile = "commit-msg.txt"
)
