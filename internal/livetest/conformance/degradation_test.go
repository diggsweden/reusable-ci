// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build live

package conformance_test

// PAR-CAP-2/3: what a forge that *lacks* a capability actually does.
//
// docs/providers.md promises specific behaviour for each gap — SARIF upload
// emits a notice and exits 0, provenance refuses rather than emitting a
// dishonest predicate — and until now nothing drove an unmet capability on a
// real forge to check. A gap that errors cryptically and a gap that silently
// succeeds are both failures, and both look identical to a suite that only
// asserts "the command did not blow up".
//
// These drive the shipped binary rather than a role, because the degradation
// decision is taken above the adapter: the capability gate lives in the CLI's
// dependency wiring, and what the user sees is an exit code and a message.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

// PAR-CAP-2: SARIF upload on a forge without code scanning.
//
// The documented degradation is a notice and exit 0 — findings route to the
// step summary or an artifact instead — and crucially *no failed API call*. A
// forge that 404s here would be the bug: it would mean the CLI tried anyway.
func TestDegradation_SARIFUpload_NoticesAndSucceeds(t *testing.T) {
	for _, kind := range forgesLacking(t, func(c provider.Capabilities) bool { return c.SARIFUpload }, "SARIF upload") {
		t.Run(string(kind), func(t *testing.T) {
			target := livetest.Accept(t, kind)
			repo := livetest.NewScratchRepo(t, target, "degrade-sarif")

			sarif := filepath.Join(t.TempDir(), "findings.sarif")
			if err := os.WriteFile(sarif, []byte(`{"version":"2.1.0","runs":[]}`), 0o600); err != nil {
				t.Fatal(err)
			}

			run := livetest.CLI(t, target, repo,
				"security", "report", "upload-sarif",
				"--sarif-file", sarif,
				"--repository", livetest.RepoSlug(target, repo),
			)

			if run.ExitCode != 0 {
				t.Fatalf("%s: unmet SARIF capability exited %d, want 0 (degradation is not a failure)\nstderr: %s",
					target.Kind, run.ExitCode, run.Stderr)
			}

			// Something must be said. A silent no-op is the failure mode this
			// scenario exists to catch: the user would believe findings were
			// uploaded.
			if strings.TrimSpace(run.Combined()) == "" {
				t.Errorf("%s: degraded silently; the user is told nothing about where findings went", target.Kind)
			}
		})
	}
}

// PAR-CAP-3: provenance is the capability that does NOT degrade.
//
// `release provenance` uses no forge attestation API: it builds the statement
// from the checksums file plus the run context every provider resolves, and
// cosign signs it. So the scenario is equivalence, not refusal — the same
// subject and the same predicate type on every forge, with only the forge's own
// context differing.
//
// Written this way round deliberately. Asserting a refusal here passes for the
// wrong reason: every error this command emits is prefixed "provenance:", so a
// missing --started-on satisfies "non-zero, and the message names the reason"
// while proving nothing about capabilities.
func TestProvenance_GeneratesEquivalentlyOnEveryForge(t *testing.T) {
	// A fixed timestamp: the statement must differ between forges only in the
	// context the forge supplies, and an unset --started-on is a usage error
	// rather than a degradation.
	const startedOn = "2026-01-01T00:00:00Z"

	subjects := map[provider.Platform]string{}

	for _, kind := range livetest.LiveForges() {
		t.Run(string(kind), func(t *testing.T) {
			target := livetest.Accept(t, kind)
			repo := livetest.NewScratchRepo(t, target, "provenance-parity")

			checksums := filepath.Join(t.TempDir(), "checksums.txt")
			body := "0000000000000000000000000000000000000000000000000000000000000000  artifact.tar.gz\n"

			if err := os.WriteFile(checksums, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}

			run := livetest.CLI(t, target, repo,
				"release", "provenance",
				"--checksum-file", checksums,
				"--go-sum", "",
				"--started-on", startedOn,
			)

			if run.ExitCode != 0 {
				t.Fatalf("%s: provenance exited %d, want 0 — it depends on no forge capability\nstderr: %s",
					target.Kind, run.ExitCode, run.Stderr)
			}

			var statement struct {
				Type          string `json:"_type"`
				PredicateType string `json:"predicateType"`
				Subject       []struct {
					Name   string            `json:"name"`
					Digest map[string]string `json:"digest"`
				} `json:"subject"`
			}

			if err := json.Unmarshal([]byte(run.Stdout), &statement); err != nil {
				t.Fatalf("%s: provenance is not a JSON statement: %v\nstdout: %s", target.Kind, err, run.Stdout)
			}

			if statement.PredicateType != provenance.PredicateTypeV1 {
				t.Errorf("%s: predicateType = %q, want %q — the predicate is forge-neutral",
					target.Kind, statement.PredicateType, provenance.PredicateTypeV1)
			}

			if len(statement.Subject) != 1 {
				t.Fatalf("%s: %d subjects, want the one artifact in the checksums file",
					target.Kind, len(statement.Subject))
			}

			subjects[kind] = statement.Subject[0].Name + "@" + statement.Subject[0].Digest["sha256"]
		})
	}

	// The subject comes from the checksums file, not from the forge, so every
	// forge must have named the same artifact and digest.
	for kind, got := range subjects {
		for other, want := range subjects {
			if got != want {
				t.Errorf("subject differs by forge: %s says %q, %s says %q — the subject is read from the checksums file",
					kind, got, other, want)
			}
		}
	}
}

// forgesLacking is forgesClaiming's mirror: the forges that do *not* claim a
// capability, which is where degradation is observable. A capability every live
// forge claims makes the scenario vacuous, and that is reported rather than
// passing quietly.
func forgesLacking(t *testing.T, claims func(provider.Capabilities) bool, capability string) []provider.Platform {
	t.Helper()

	var kinds []provider.Platform

	for _, kind := range livetest.LiveForges() {
		capabilities, known := livetest.Capabilities(kind)
		if !known {
			t.Fatalf("no adapter for platform %q", kind)
		}

		if claims(capabilities) {
			t.Logf("SKIP %s: claims %s, so there is no degradation to observe", kind, capability)

			continue
		}

		kinds = append(kinds, kind)
	}

	if len(kinds) == 0 {
		t.Fatalf("every live forge claims %s, so this degradation scenario proved nothing", capability)
	}

	return kinds
}
