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
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// PAR-CAP-3: provenance on a forge with no build profile.
//
// Here the documented behaviour is the opposite of SARIF's: refuse, clearly,
// rather than emit a predicate that claims a build it cannot describe. Getting
// these two the same way round would be the defect — the difference is the
// whole point of degradation being designed rather than incidental.
func TestDegradation_Provenance_RefusesClearly(t *testing.T) {
	for _, kind := range forgesLacking(t, func(c provider.Capabilities) bool { return c.Attestation }, "provenance profile") {
		t.Run(string(kind), func(t *testing.T) {
			target := livetest.Accept(t, kind)

			// Only GitLab lacks a provenance profile among the live forges;
			// Forgejo has one, so it is filtered out by the capability gate
			// above and never reaches here.
			if kind != provider.PlatformGitLab {
				t.Skipf("%s supplies a provenance profile; nothing to refuse", kind)
			}

			repo := livetest.NewScratchRepo(t, target, "degrade-provenance")

			checksums := filepath.Join(t.TempDir(), "checksums.txt")
			body := "0000000000000000000000000000000000000000000000000000000000000000  artifact.tar.gz\n"

			if err := os.WriteFile(checksums, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}

			run := livetest.CLI(t, target, repo,
				"release", "provenance",
				"--checksum-file", checksums,
				"--go-sum", "",
			)

			if run.ExitCode == 0 {
				t.Fatalf("%s: emitted provenance it has no profile for\nstdout: %s", target.Kind, run.Stdout)
			}

			// "Refuses" must mean a stated reason, not a stack trace or a bare
			// non-zero exit.
			if !strings.Contains(strings.ToLower(run.Stderr), "unsupported") &&
				!strings.Contains(strings.ToLower(run.Stderr), "provenance") {
				t.Errorf("%s: refusal does not name the reason\nstderr: %s", target.Kind, run.Stderr)
			}
		})
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
