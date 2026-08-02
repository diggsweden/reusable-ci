// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

// flagWorkflow is the shared --workflow flag name used by the isolation,
// job-graph and pin-reachability verbs; the `validate workflow` subcommand
// tree deliberately reuses the same word. Declared once so spellings cannot
// drift and the package stays under goconst's literal budget. The value is
// part of the CLI contract — docs/cli-reference.md is generated from it and
// a sync test gates any change.
const flagWorkflow = "workflow"
