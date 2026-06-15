// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

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
//   - workflow.go      — `workflow <input-defaults|contract-residue>`
//   - changelog.go     — changelog
//   - event_context.go — refuse PR-context triggers on privileged workflows
package validate

import "github.com/urfave/cli/v3"

// New returns the `validate` subgroup command tree.
//
// Verb convention across the CLI:
//
//   - validate  — assert a STATIC precondition from local state alone
//     (semver format, ref-type, namespace, secret presence, token scopes).
//     These live here in the reusable `validate` group.
//   - verify-*  — RECOMPUTE or CROSS-CHECK an already-produced or remote
//     thing against expected truth (e.g. `release verify-dist` recomputes
//     the dist digest, `release verify-tag` re-checks the remote,
//     `container ledger verify-digests` cross-checks registry digests). These live
//     under the command that owns the produced artefact, not here.
//
// Signature checks (`validate tag signature`, `validate artifact-signature`,
// `validate container-signature`) are cross-checks by nature but stay in this
// group as the single "checks" namespace; their Usage says "verify" to mark
// the distinction.
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
