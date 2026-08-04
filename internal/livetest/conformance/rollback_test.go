// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build live

package conformance_test

// PAR-REG-4: undoing a promotion.
//
// Rollback is the most destructive verb in the product and the least exercised:
// it runs when a release has already gone wrong, which is exactly when a second
// mistake is least affordable. Its safety rules are all *conditional on registry
// state* — delete only a tag that still serves the digest this promotion pushed,
// delete an immutable tag only when the promotion created it, restore a moving
// tag to the digest it held before. None of those conditions exist against a
// fake registry; they are the registry.
//
// There are two rollback paths and they undo different things:
//
//   - from the ledger: deletes the stage's own pointer (<base>:release), derived
//     from the ledger and the stage rather than recorded anywhere;
//   - from a promotion journal: undoes a ledger-release-tag promotion, which is
//     the only path that can restore a moving tag, because what it pointed at
//     before is not recoverable from the ledger once the promotion has run.
//
// Both run on every forge that claims tag deletion, so this is a real GitLab and
// Forgejo comparison.

import (
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

// deletesTags selects the forges that claim the capability rollback needs.
func deletesTags(c provider.Capabilities) bool { return c.ContainerTagDeletion }

// rollbackFixture is the shared setup: a scratch repository, a working
// directory the CLI runs in, and registry credentials. Each scenario pushes its
// own images, because what is in the registry beforehand is the variable under
// test.
type rollbackFixture struct {
	target    livetest.Target
	repo      string
	imagePath string
	work      string
	authFile  string
	opts      livetest.RunOptions
}

func newRollbackFixture(t *testing.T, kind provider.Platform, name string) rollbackFixture {
	t.Helper()

	target := livetest.Accept(t, kind)

	registry, err := livetest.RegistryHost(target)
	if err != nil {
		t.Fatal(err)
	}

	repo := livetest.NewScratchRepo(t, target, name)
	work := t.TempDir()

	return rollbackFixture{
		target:    target,
		repo:      repo,
		imagePath: registry + "/" + target.Owner + "/" + repo,
		work:      work,
		authFile:  livetest.RegistryAuthFile(t, target, work),
		opts:      livetest.RunOptions{Dir: work},
	}
}

func (f rollbackFixture) run(t *testing.T, args ...string) livetest.Run {
	t.Helper()

	return livetest.CLIIn(t, f.target, f.repo, f.opts, args...)
}

// mustRun fails the test when the command did not succeed, naming the verb so a
// failure says which step of the flow broke.
func (f rollbackFixture) mustRun(t *testing.T, verb string, args ...string) {
	t.Helper()

	run := f.run(t, args...)
	if run.ExitCode != 0 {
		t.Fatalf("%s %s exited %d\nstderr: %s", f.target.Kind, verb, run.ExitCode, run.Stderr)
	}
}

// digest asserts what the registry serves for a tag, or that it serves nothing.
func (f rollbackFixture) assertServes(t *testing.T, tag, want, why string) {
	t.Helper()

	got, found := livetest.ImageDigest(t, f.target, f.repo, tag)
	if !found {
		t.Errorf("%s: %s:%s serves nothing — %s", f.target.Kind, f.imagePath, tag, why)

		return
	}

	if got != want {
		t.Errorf("%s: %s:%s serves %s, want %s — %s", f.target.Kind, f.imagePath, tag, got, want, why)
	}
}

func (f rollbackFixture) assertAbsent(t *testing.T, tag, why string) {
	t.Helper()

	if _, found := livetest.ImageDigest(t, f.target, f.repo, tag); found {
		t.Errorf("%s: %s:%s still serves an image — %s", f.target.Kind, f.imagePath, tag, why)
	}
}

// PAR-REG-4a: rolling back a stage removes that stage's pointer and nothing
// else. The immutable release tag is not a stage destination, so a rollback that
// took it would be destroying the release it was asked to un-promote.
func TestRollback_FromLedger_RemovesTheStagePointerOnly(t *testing.T) {
	const (
		releaseTag   = "v0.0.1"
		candidateTag = "staging-" + releaseTag
		stage        = "release"
	)

	for _, kind := range forgesClaiming(t, deletesTags, "container tag deletion") {
		t.Run(string(kind), func(t *testing.T) {
			f := newRollbackFixture(t, kind, "rollback-stage")

			// One manifest under the candidate and the immutable release tag,
			// as a build leaves it.
			pushed := livetest.PushImageTags(t, f.target, f.repo, candidateTag, releaseTag)
			ledger := "release-images.json"

			f.mustRun(t, "ledger add",
				"container", "ledger", "add",
				"--ledger", ledger, "--auth-file", f.authFile, "--tag", releaseTag,
				"--kind", "distroless",
				"--candidate-tag", f.imagePath+":"+candidateTag,
				"--final-tag", f.imagePath+":"+releaseTag,
				"--sbom", writeSBOM(t, f.work, "rollback-stage"),
				"--capture-digest",
			)

			f.mustRun(t, "ledger promote",
				"container", "ledger", "promote",
				"--ledger", ledger, "--auth-file", f.authFile, "--tag", releaseTag, "--stage", stage)
			f.assertServes(t, stage, pushed.Digest, "promotion should have moved the pointer here")

			f.mustRun(t, "ledger rollback",
				"container", "ledger", "rollback",
				"--ledger", ledger, "--auth-file", f.authFile, "--tag", releaseTag, "--stage", stage)

			f.assertAbsent(t, stage, "rollback should have removed the stage pointer")
			f.assertServes(t, releaseTag, pushed.Digest,
				"the immutable release tag is not a stage destination and must survive rollback")
			f.assertServes(t, candidateTag, pushed.Digest,
				"the candidate is the promotion source and must survive rollback")
		})
	}
}

// PAR-REG-4b: the journal's central judgement — an immutable tag may be deleted
// only when this promotion created it.
//
// The journal exists because that fact is unrecoverable afterwards: once the tag
// is written, the registry cannot say who wrote it. Here the release tag does
// not exist before the promotion, so undoing means removing it.
func TestRollback_FromJournal_RemovesAReleaseTagThePromotionCreated(t *testing.T) {
	const (
		releaseTag   = "v0.0.1"
		candidateTag = "staging-" + releaseTag
	)

	for _, kind := range forgesClaiming(t, deletesTags, "container tag deletion") {
		t.Run(string(kind), func(t *testing.T) {
			f := newRollbackFixture(t, kind, "rollback-journal")

			// Only the candidate is pushed: the release tag is what the
			// promotion will create, and therefore what rollback may remove.
			pushed := livetest.PushImage(t, f.target, f.repo, candidateTag)

			ledger := "release-images.json"
			journal := filepath.Join(f.work, "promotion-journal.jsonl")

			f.mustRun(t, "ledger add",
				"container", "ledger", "add",
				"--ledger", ledger, "--auth-file", f.authFile, "--tag", releaseTag,
				"--kind", "distroless",
				"--candidate-tag", f.imagePath+":"+candidateTag,
				"--final-tag", f.imagePath+":"+releaseTag,
				"--sbom", writeSBOM(t, f.work, "rollback-journal"),
				"--capture-digest",
			)

			// The journal is only defined for a release promotion using the
			// ledger's own release tags — the sign-before-publish model.
			f.mustRun(t, "ledger promote",
				"container", "ledger", "promote",
				"--ledger", ledger, "--auth-file", f.authFile, "--tag", releaseTag,
				"--stage", "release", "--release-tags-from-ledger", "--journal", journal)

			f.assertServes(t, releaseTag, pushed.Digest, "promotion should have published the release tag")

			f.mustRun(t, "ledger rollback",
				"container", "ledger", "rollback",
				"--ledger", ledger, "--auth-file", f.authFile, "--tag", releaseTag, "--journal", journal)

			f.assertAbsent(t, releaseTag, "the promotion created this tag, so rollback should remove it")
			f.assertServes(t, candidateTag, pushed.Digest,
				"the candidate is the promotion source and must survive rollback")
		})
	}
}

// PAR-REG-4c: rollback restores a moving tag rather than deleting it.
//
// This is the property nothing below a real registry can check. A moving tag
// that pointed at the previous release must go back to pointing at it — not be
// deleted, which would leave consumers of `:stable` with nothing, and not be
// left on the failed release, which would leave them on the image being rolled
// back. Deleting instead of restoring is a plausible implementation and is
// indistinguishable from correct until a registry is asked.
func TestRollback_FromJournal_RestoresAMovingTagToItsPreviousImage(t *testing.T) {
	const (
		releaseTag   = "v0.0.1"
		candidateTag = "staging-" + releaseTag
		movingTag    = "stable"
	)

	for _, kind := range forgesClaiming(t, deletesTags, "container tag deletion") {
		t.Run(string(kind), func(t *testing.T) {
			f := newRollbackFixture(t, kind, "rollback-moving")

			// The release already in production, and the candidate that is
			// about to replace it. Distinct images, so a restore is provable.
			//
			// The previous release carries its own immutable version tag as
			// well as the moving pointer, because that is what the build-once
			// model produces — and because it is load-bearing: a registry keeps
			// a manifest only while some tag references it, and Forgejo drops
			// one the moment none does. Without the version tag the previous
			// image would be unrecoverable there, which is a property of the
			// fixture rather than of rollback.
			const previousRelease = "v0.0.0"

			previous := livetest.PushImageTags(t, f.target, f.repo, previousRelease, movingTag)
			candidate := livetest.PushImage(t, f.target, f.repo, candidateTag)

			if previous.Digest == candidate.Digest {
				t.Fatal("fixture pushed identical images; the restore would be unobservable")
			}

			ledger := "release-images.json"
			journal := filepath.Join(f.work, "promotion-journal.jsonl")

			f.mustRun(t, "ledger add",
				"container", "ledger", "add",
				"--ledger", ledger, "--auth-file", f.authFile, "--tag", releaseTag,
				"--kind", "distroless",
				"--candidate-tag", f.imagePath+":"+candidateTag,
				"--final-tag", f.imagePath+":"+releaseTag,
				"--moving-tag", f.imagePath+":"+movingTag,
				"--sbom", writeSBOM(t, f.work, "rollback-moving"),
				"--capture-digest",
			)

			f.mustRun(t, "ledger promote",
				"container", "ledger", "promote",
				"--ledger", ledger, "--auth-file", f.authFile, "--tag", releaseTag,
				"--stage", "release", "--release-tags-from-ledger", "--journal", journal)

			f.assertServes(t, movingTag, candidate.Digest, "promotion should have moved the pointer to the candidate")

			f.mustRun(t, "ledger rollback",
				"container", "ledger", "rollback",
				"--ledger", ledger, "--auth-file", f.authFile, "--tag", releaseTag, "--journal", journal)

			f.assertServes(t, movingTag, previous.Digest,
				"rollback must put the moving tag back on the previous release, not delete it")
			f.assertAbsent(t, releaseTag, "the promotion created the release tag, so rollback should remove it")
		})
	}
}
