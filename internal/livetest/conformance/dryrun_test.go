// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build live

package conformance_test

// PAR-UX-2: --dry-run mutates nothing, on every destructive ledger verb.
//
// promote, cleanup and rollback all move or delete tags in a real registry, and
// --dry-run is what a release engineer reaches for before letting any of them
// run. Nothing has ever checked that the flag is honoured end to end: a fake
// registry cannot, because the decorator that implements dry-run is exactly the
// thing under test, and a unit test of it asserts the same assumption twice.
//
// Each case is self-validating. Asserting "the registry is unchanged" is trivial
// to pass — a verb that silently did nothing at all would pass it, and so would
// a scenario whose preconditions were wrong. So every case then runs the SAME
// command for real and requires the registry to change. The dry-run assertion
// only means something because the wet run proves there was something to skip.
//
// Snapshots compare the whole repository rather than the tags a verb was
// expected to touch: the promise is that nothing was mutated, not that the
// predicted mutation was skipped.

import (
	"maps"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

func TestDryRun_DestructiveLedgerVerbs_MutateNothing(t *testing.T) {
	const (
		releaseTag   = "v0.0.1"
		candidateTag = "staging-" + releaseTag
		promotedTag  = "release"
	)

	// Each verb needs a registry state in which it would really act; a verb with
	// nothing to do proves nothing about dry-run.
	cases := []struct {
		verb string

		// promoteFirst puts a real :release pointer in place, which is the
		// precondition rollback needs to have something to undo.
		promoteFirst bool

		args []string
	}{
		{
			verb: "promote",
			args: []string{"promote", "--stage", promotedTag},
		},
		{
			// cleanup deletes the candidate once the final tag serves the
			// digest, which it does from the initial push.
			verb: "cleanup",
			args: []string{"cleanup"},
		},
		{
			verb:         "rollback",
			promoteFirst: true,
			args:         []string{"rollback", "--stage", promotedTag},
		},
	}

	for _, kind := range forgesClaiming(t, deletesTags, "container tag deletion") {
		t.Run(string(kind), func(t *testing.T) {
			for _, testCase := range cases {
				t.Run(testCase.verb, func(t *testing.T) {
					target := livetest.Accept(t, kind)
					f := newLedgerFixture(t, kind, "dryrun-"+testCase.verb)

					livetest.PushImageTags(t, f.target, f.repo, candidateTag, releaseTag)

					ledger := "release-images.json"

					f.mustRun(t, "ledger add",
						"container", "ledger", "add",
						"--ledger", ledger, "--auth-file", f.authFile, "--tag", releaseTag,
						"--role", "distroless",
						"--candidate-tag", f.imagePath+":"+candidateTag,
						"--final-tag", f.imagePath+":"+releaseTag,
						"--sbom", writeSBOM(t, f.work, "dryrun-"+testCase.verb),
						"--capture-digest",
					)

					if testCase.promoteFirst {
						f.mustRun(t, "ledger promote",
							"container", "ledger", "promote",
							"--ledger", ledger, "--auth-file", f.authFile,
							"--tag", releaseTag, "--stage", promotedTag)
					}

					args := append([]string{"container", "ledger"}, testCase.args...)
					args = append(args, "--ledger", ledger, "--auth-file", f.authFile, "--tag", releaseTag)

					before := livetest.RegistrySnapshot(t, target, f.repo)

					preview := f.run(t, append(args, "--dry-run")...)
					if preview.ExitCode != 0 {
						t.Fatalf("%s ledger %s --dry-run exited %d\nstderr: %s",
							kind, testCase.verb, preview.ExitCode, preview.Stderr)
					}

					// A dry-run that says nothing is a failure of its own: the
					// output IS the deliverable, since the point is to show what
					// would happen before it does.
					if !strings.Contains(preview.Combined(), "[dry-run]") {
						t.Errorf("%s ledger %s --dry-run printed no plan, so it previewed nothing a reader can check\nstdout: %s\nstderr: %s",
							kind, testCase.verb, preview.Stdout, preview.Stderr)
					}

					after := livetest.RegistrySnapshot(t, target, f.repo)
					if !maps.Equal(before, after) {
						t.Errorf("%s ledger %s --dry-run mutated the registry\nbefore: %v\nafter:  %v",
							kind, testCase.verb, before, after)
					}

					// The control. Without it, a verb that did nothing at all
					// would pass everything above.
					f.mustRun(t, "ledger "+testCase.verb, args...)

					wet := livetest.RegistrySnapshot(t, target, f.repo)
					if maps.Equal(before, wet) {
						t.Errorf("%s ledger %s changed nothing when run for real, so the dry-run assertion proved nothing\nstate: %v",
							kind, testCase.verb, wet)
					}
				})
			}
		})
	}
}
