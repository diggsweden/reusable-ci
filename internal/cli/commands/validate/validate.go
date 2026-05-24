// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package validate wires `reusable-ci validate ...` subcommands using
// urfave/cli v3.
//
// The subcommand tree is grouped by responsibility:
//
//   - prerequisites.go — the concurrent orchestrator
//   - tags.go          — `tag <format|uniqueness|commit|signature>` plus refType
//   - tokens.go        — `auth <token|bot-permissions|user>`
//   - secrets.go       — `secret <gpg-public-key|maven-central>`
//   - cargo.go         — cargo
//   - jvmreproducibility.go — jvm-reproducibility
//   - workflow.go      — `workflow <input-defaults|v3-contracts>`
//   - changelog.go     — changelog
//   - event_context.go — refuse PR-context triggers on privileged workflows
package validate

import "github.com/urfave/cli/v3"

// New returns the `validate` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "validate",
		Usage: "input / state validators (tag, workflow, auth, secret, …)",
		Commands: []*cli.Command{
			prerequisitesCmd(),
			refTypeCmd(),
			tagGroup(),
			workflowGroup(),
			changelogCmd(),
			authGroup(),
			secretGroup(),
			cargoCmd(),
			jvmReproducibilityCmd(),
			artifactSignatureCmd(),
			containerSignatureCmd(),
			eventContextCmd(),
		},
	}
}
