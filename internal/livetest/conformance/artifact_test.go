// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build live

package conformance_test

// PAR-ART-*: run artifacts, where the interesting claim is about the refusal.
//
// GitHub and Forgejo expose an intra-run artifact store; GitLab does not, and
// that is a platform difference rather than a missing adapter — GitLab job
// artifacts are declared in .gitlab-ci.yml and uploaded by the runner, with no
// public API for a job to push one. So there is nothing to implement, and what
// parity means here is that the *refusal* is honest and identically shaped.
//
// The positive path is deliberately not here. Uploading needs the run context a
// runner supplies, so a host-run scenario could only observe the runtime being
// absent — that assertion belongs in the in-runner tier. What a host CAN prove is
// the distinction that actually confuses adopters: "this forge will never do
// this" versus "you are not in a run". Those must not look alike, and PAR-ART-2
// is the only place that is checked.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

func claimsRunArtifacts(c provider.Capabilities) bool { return c.RunArtifacts }

// artifactUploadArgs is one upload invocation against a real file, so the verb
// gets past its own input validation and reaches the capability gate.
func artifactUploadArgs(t *testing.T) []string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "report.txt"), []byte("livetest\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	return []string{"artifact", "upload", "--name", "livetest-par-art", "--dir", dir}
}

// PAR-ART-1: a forge without an artifact store refuses, and says what is
// missing.
//
// The failure this guards is not an error — it is a *silent success*. A verb
// that exits 0 having uploaded nothing leaves a pipeline believing its reports
// were published, and the next job looking for them is where anyone finds out.
func TestArtifact_WithoutAStore_RefusesAndNamesTheGap(t *testing.T) {
	for _, kind := range forgesLacking(t, claimsRunArtifacts, "run artifacts") {
		t.Run(string(kind), func(t *testing.T) {
			target := livetest.Accept(t, kind)
			repo := livetest.NewScratchRepo(t, target, "artifact-refuse")

			run := livetest.CLI(t, target, repo, artifactUploadArgs(t)...)

			if run.ExitCode == 0 {
				t.Fatalf("%s: uploading to a forge with no artifact store exited 0\nstdout: %s", kind, run.Stdout)
			}

			// A refusal has to be readable as one. Naming the platform is what
			// separates "this forge cannot" from a transient failure the reader
			// would otherwise sit and retry.
			lower := strings.ToLower(run.Stderr)
			if !strings.Contains(lower, "unsupported") && !strings.Contains(lower, "not supported") {
				t.Errorf("%s: refusal does not say the capability is unsupported\nstderr: %s", kind, run.Stderr)
			}

			if !strings.Contains(lower, string(kind)) {
				t.Errorf("%s: refusal does not name the platform, so a reader cannot tell why\nstderr: %s",
					kind, run.Stderr)
			}
		})
	}
}

// PAR-ART-2: a forge that HAS an artifact store must not refuse as though it
// did not.
//
// Off a runner the upload cannot succeed — there is no run to attach to — and
// that is fine. What must not happen is the two being reported the same way: an
// adopter who sees "unsupported on this platform" on a forge that supports it
// perfectly well concludes the feature is missing and works around a problem
// that does not exist.
func TestArtifact_WithAStore_FailsOnTheRuntimeNotTheCapability(t *testing.T) {
	for _, kind := range forgesClaiming(t, claimsRunArtifacts, "run artifacts") {
		t.Run(string(kind), func(t *testing.T) {
			target := livetest.Accept(t, kind)
			repo := livetest.NewScratchRepo(t, target, "artifact-runtime")

			run := livetest.CLI(t, target, repo, artifactUploadArgs(t)...)

			// Succeeding would mean a run context leaked into the suite, which
			// is worth knowing but is not this scenario's claim.
			if run.ExitCode == 0 {
				t.Skipf("%s: upload succeeded, so a run context was present", kind)
			}

			lower := strings.ToLower(run.Stderr)
			if strings.Contains(lower, "unsupported on this platform") {
				t.Errorf("%s claims run artifacts but the CLI refuses them as unsupported\nstderr: %s",
					kind, run.Stderr)
			}

			// The honest answer here is "you are not in a CI run", which the
			// exit ladder classifies as a usage problem rather than an outage.
			if run.ExitCode == int(errs.ExitCodeUnavailable) {
				t.Errorf("%s: exits %d (unavailable) off a runner, which a caller cannot tell from an outage\nstderr: %s",
					kind, run.ExitCode, run.Stderr)
			}
		})
	}
}
