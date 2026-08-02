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

import (
	"slices"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/cmdmeta"
)

// New returns the `validate` subgroup command tree.
//
// Verb convention across the CLI:
//
//   - validate  — a fail-fast PRECONDITION gate run before you act. Most
//     checks are local (semver format, ref-type, namespace, secret presence),
//     but some legitimately reach out read-only (`auth token` hits the platform
//     API, `tag commit` checks remote reachability). The defining trait is
//     "gate the next step," not "offline." These live here in the reusable
//     `validate` group.
//   - verify-*  — RECOMPUTE or CROSS-CHECK an already-produced or remote
//     thing against expected truth (e.g. `release verify-dist` recomputes
//     the dist digest, `release verify-tag` re-checks the remote,
//     `container ledger verify-digests` cross-checks registry digests). These live
//     under the command that owns the produced artefact, not here.
//
// Several validate subcommands read "verify …" in their Usage (signature checks,
// remote-reachability checks); that wording is intentional where the check
// cross-checks something rather than asserting a local fact.
func New() *cli.Command {
	return &cli.Command{
		Name:  "validate",
		Usage: "fail-fast pre-flight validators (tag, workflow, auth, secret, …)",
		Description: `Verb convention across the CLI — three distinct concerns:

   validate <x>      a fail-fast PRECONDITION gate run before you act. Mostly
                     local (semver format, ref-type, secret presence), but some
                     reach out read-only (` + "`auth token`" + ` hits the platform API,
                     ` + "`tag commit`" + ` checks remote reachability). The point is to gate
                     the next step, not to be offline. (Here.)
   release verify-*  RE-verify an already-produced or remote thing at the trust
                     boundary, against tampering between jobs: ` + "`verify-tag`" + ` re-checks
                     the remote tag still points to the release commit, ` + "`verify-dist`" + `
                     recomputes the dist digest. Lives under the command that owns
                     the artefact, not here.
   report status *   render a step-summary block (no checking).

Some validate subcommands read "verify …" in their Usage (signature checks,
remote-reachability checks); that is intentional where the check cross-checks
something rather than asserting a local fact.`,
		Commands: slices.Concat(
			cmdmeta.WithCategory("Release pre-flight gates",
				prerequisitesCmd(), refTypeCmd(), eventContextCmd(), isolationCmd(), jobGraphCmd(), pinReachabilityCmd(), tagGroup(), workflowGroup(), changelogCmd()),
			cmdmeta.WithCategory("Auth & secrets", authGroup(), secretGroup()),
			cmdmeta.WithCategory("Signature verification", artifactSignatureCmd(), containerSignatureCmd(), containerAttestationCmd()),
			cmdmeta.WithCategory("Ecosystem checks", cargoCmd(), jvmReproducibilityCmd()),
		),
	}
}
