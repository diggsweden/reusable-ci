// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build live

package conformance_test

// Setup shared by the ledger scenarios. Kept out of the scenario files so that
// each of those reads as the claim it makes, and so a helper used by four tests
// is not owned by whichever one was written first.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

// ledgerFixture is the setup every ledger scenario needs: a scratch repository,
// a working directory the CLI runs in, and registry credentials. Each scenario
// pushes its own images, because what is in the registry beforehand is the
// variable under test.
//
// It began life inside the rollback scenarios and was named for them. Four
// files use it now, so it lives here under a name that says what it is rather
// than which test happened to need it first.
type ledgerFixture struct {
	target    livetest.Target
	repo      string
	imagePath string
	work      string
	authFile  string
	opts      livetest.RunOptions
}

func newLedgerFixture(t *testing.T, kind provider.Platform, name string) ledgerFixture {
	t.Helper()

	target := livetest.Accept(t, kind)

	registry, err := livetest.RegistryHost(target)
	if err != nil {
		t.Fatal(err)
	}

	repo := livetest.NewScratchRepo(t, target, name)
	work := t.TempDir()

	return ledgerFixture{
		target:    target,
		repo:      repo,
		imagePath: registry + "/" + target.Owner + "/" + repo,
		work:      work,
		authFile:  livetest.RegistryAuthFile(t, target, work),
		opts:      livetest.RunOptions{Dir: work},
	}
}

func (f ledgerFixture) run(t *testing.T, args ...string) livetest.Run {
	t.Helper()

	return livetest.CLIIn(t, f.target, f.repo, f.opts, args...)
}

// mustRun fails the test when the command did not succeed, naming the verb so a
// failure says which step of the flow broke.
func (f ledgerFixture) mustRun(t *testing.T, verb string, args ...string) {
	t.Helper()

	run := f.run(t, args...)
	if run.ExitCode != 0 {
		t.Fatalf("%s %s exited %d\nstderr: %s", f.target.Kind, verb, run.ExitCode, run.Stderr)
	}
}

// digest asserts what the registry serves for a tag, or that it serves nothing.
func (f ledgerFixture) assertServes(t *testing.T, tag, want, why string) {
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

func (f ledgerFixture) assertAbsent(t *testing.T, tag, why string) {
	t.Helper()

	if _, found := livetest.ImageDigest(t, f.target, f.repo, tag); found {
		t.Errorf("%s: %s:%s still serves an image — %s", f.target.Kind, f.imagePath, tag, why)
	}
}

// writeSBOM produces a minimal CycloneDX document at a path the ledger's schema
// accepts (it requires a *.cyclonedx.json name). Content is not inspected by the
// ledger — the entry records the path so a later step can find it.
func writeSBOM(t *testing.T, dir, flavour string) string {
	t.Helper()

	name := "image-sbom-" + flavour + ".cyclonedx.json"
	body := `{"bomFormat":"CycloneDX","specVersion":"1.5","version":1,"components":[]}`

	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	// Relative on purpose: the ledger refuses an absolute path, because the
	// entry is read later from wherever the release artifacts are.
	return name
}

// writeToken puts a credential in a file, because that is how the product takes
// one: a token on a command line would be visible in every process listing.
func writeToken(t *testing.T, token string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}
