// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build live

// Package conformance_test holds the live-forge conformance scenarios: one
// scenario written once, run against every forge, asserting the same outcome.
//
// The separation from the black-box suite is structural, not just a build tag.
// Toolchain and artifact claims live in the reusable-ci-blackbox-tests
// repository; forge-API claims live here under internal/livetest/. Nothing in
// this directory should ever need syft, gpg, or a reproducible archive, and
// nothing there should ever need a forge token.
package conformance_test

import (
	"context"
	"encoding/binary"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
	"github.com/diggsweden/reusable-ci/v3/internal/livetest/rawref"
)

// forgesClaiming returns the live forges whose Capabilities() claim what a
// scenario needs, saying out loud which were skipped and why.
//
// This is the shape that keeps the tier a parity suite rather than N suites
// that drift: a scenario is one body iterating this list, and the capability
// decides where it runs. A silent skip would be worse than no test, so an
// unclaimed capability is reported, and a scenario no forge can run is a
// failure rather than a quiet pass.
func forgesClaiming(t *testing.T, needs func(provider.Capabilities) bool, capability string) []provider.ForgeAPI {
	t.Helper()

	var forges []provider.ForgeAPI

	for _, forge := range livetest.LiveForges(t) {
		capabilities, known := livetest.Capabilities(forge)
		if !known {
			t.Fatalf("no adapter for platform %q", forge)
		}

		if !needs(capabilities) {
			t.Logf("SKIP %s: does not claim %s", forge, capability)

			continue
		}

		forges = append(forges, forge)
	}

	if len(forges) == 0 {
		t.Fatalf("no live forge claims %s, so this scenario proved nothing", capability)
	}

	return forges
}

func claimsReleaseAssets(c provider.Capabilities) bool { return c.ReleaseAssets }

// PAR-REL-1: create a release with assets, on every forge, and prove through
// the forge's own API that the same thing arrived.
//
// The assertions are deliberately about *outcome*, not about calls. Forgejo
// attaches assets to the release; GitLab models them as generic-package links.
// Those are different requests reaching the same place, and hiding exactly that
// difference is what the adapter is for — so what is compared is the tag, the
// name, the body, the asset names, and the SHA-256 of the bytes each forge
// serves back. A forge that reports an asset it corrupted fails here.
func TestRelease_CreateWithAssets_IsEquivalentAcrossForges(t *testing.T) {
	const (
		tag  = "v0.0.1-rc-live"
		body = "livetest PAR-REL-1\n\nbody text with a second line.\n"
	)

	for _, forge := range forgesClaiming(t, claimsReleaseAssets, "release assets") {
		t.Run(string(forge), func(t *testing.T) {
			// No t.Parallel: one lab is a single mutable fixture, and two
			// forges sharing it is not the property under test.
			target := livetest.Accept(t, forge)
			repo := livetest.NewScratchRepo(t, target, "release-assets")

			// Both adapters release an existing tag rather than creating one,
			// so the fixture has to supply the commit and the tag first.
			livetest.PrepareTag(t, target, repo, tag)

			assets, digests := writeAssets(t)

			adapter := livetest.Provider(t, target, repo)

			creator, ok := adapter.(provider.ReleaseCreator)
			if !ok {
				t.Fatalf("%s does not implement ReleaseCreator", forge)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()

			spec := provider.ReleaseSpec{
				Tag:       tag,
				Name:      "PAR-REL-1",
				NotesFile: writeNotes(t, body),
				Assets:    assets,
			}

			if err := creator.CreateRelease(ctx, livetest.RepoSlug(target, repo), spec); err != nil {
				t.Fatalf("%s CreateRelease: %v", forge, err)
			}

			// Every assertion below reads the forge directly. Going through the
			// adapter would let an adapter that misreads its own writes agree
			// with itself and pass.
			reader := rawref.Reader{
				Forge:  string(forge),
				Base:   target.BaseURL(),
				Token:  target.Token,
				Owner:  target.Owner,
				Client: livetest.HTTPClient(t, target, time.Minute),
			}

			release, found, err := rawref.ReleaseByTag(ctx, reader, repo, tag)
			if err != nil {
				t.Fatalf("%s raw release read: %v", forge, err)
			}

			if !found {
				t.Fatalf("%s reported success but has no release at %s", forge, tag)
			}

			if release.Tag != tag {
				t.Errorf("%s release tag = %q, want %q", forge, release.Tag, tag)
			}

			if release.Name != spec.Name {
				t.Errorf("%s release name = %q, want %q", forge, release.Name, spec.Name)
			}

			if release.Body != body {
				t.Errorf("%s release body = %q, want %q", forge, release.Body, body)
			}

			assertAssetsArrivedIntact(t, forge, release, assets, digests)
		})
	}
}

// writeAssets creates two assets whose content differs, so a forge that mixed
// them up cannot pass by digesting the same bytes twice.
func writeAssets(t *testing.T) ([]string, map[string]string) {
	t.Helper()

	dir := t.TempDir()
	digests := make(map[string]string, 2)
	paths := make([]string, 0, 2)

	for name, content := range map[string]string{
		"par-rel-1-first.txt":  "first asset content\n",
		"par-rel-1-second.txt": "second asset content, deliberately different\n",
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		digest, _, err := rawref.SHA256(path)
		if err != nil {
			t.Fatal(err)
		}

		digests[name] = digest

		paths = append(paths, path)
	}

	slices.Sort(paths)

	return paths, digests
}

func writeNotes(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// PAR-REL-2: UploadReleaseAsset is a role of its own, and being satisfied is
// not the same as working. PAR-REL-1 attaches assets through CreateRelease's
// spec; this attaches one to a release that already exists, which is the path a
// pipeline takes when it builds artifacts after cutting the release.
func TestRelease_UploadAssetToExistingRelease_ServesTheSameBytes(t *testing.T) {
	const tag = "v0.0.2-rc-live"

	for _, forge := range forgesClaiming(t, claimsReleaseAssets, "release assets") {
		t.Run(string(forge), func(t *testing.T) {
			// No t.Parallel: one lab is a single mutable fixture, and two
			// forges sharing it is not the property under test.
			target := livetest.Accept(t, forge)
			repo := livetest.NewScratchRepo(t, target, "release-upload")
			livetest.PrepareTag(t, target, repo, tag)

			adapter := livetest.Provider(t, target, repo)

			creator, ok := adapter.(provider.ReleaseCreator)
			if !ok {
				t.Fatalf("%s does not implement ReleaseCreator", forge)
			}

			uploader, ok := adapter.(provider.ReleaseAssetUploader)
			if !ok {
				t.Fatalf("%s claims release assets but does not implement ReleaseAssetUploader", forge)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()

			if err := creator.CreateRelease(ctx, livetest.RepoSlug(target, repo),
				provider.ReleaseSpec{Tag: tag, Name: "PAR-REL-2"}); err != nil {
				t.Fatalf("%s CreateRelease: %v", forge, err)
			}

			// Content the forge cannot have produced by accident: if the digest
			// matches, these exact bytes made the round trip.
			path := filepath.Join(t.TempDir(), "par-rel-2-late.txt")
			if err := os.WriteFile(path, []byte("uploaded after the release existed\n"), 0o600); err != nil {
				t.Fatal(err)
			}

			wantDigest, wantSize, err := rawref.SHA256(path)
			if err != nil {
				t.Fatal(err)
			}

			if err := uploader.UploadReleaseAsset(ctx, tag, path); err != nil {
				t.Fatalf("%s UploadReleaseAsset: %v", forge, err)
			}

			release := mustReadRelease(ctx, t, target, repo, tag)

			asset, found := release.Asset(filepath.Base(path))
			if !found {
				t.Fatalf("%s accepted the upload but serves no asset %q (has %v)",
					forge, filepath.Base(path), release.AssetNames())
			}

			if asset.Digest != wantDigest {
				t.Errorf("%s asset digest = %s, want %s — the forge served different bytes than were uploaded",
					forge, asset.Digest, wantDigest)
			}

			if asset.Size != wantSize {
				t.Errorf("%s asset size = %s, want %s", forge,
					rawref.FormatSize(asset.Size), rawref.FormatSize(wantSize))
			}
		})
	}
}

// PAR-REL-3: releasing a tag that already has a release.
//
// This found a real defect on its first run: GitLab's CreateRelease POSTed
// straight to /releases and returned HTTP 409 "Release already exists" on the
// second call, while github and forgejo both delete-then-create. Its own
// comment claimed a delete-if-exists step it never performed, and
// `release publish --strategy recreate` documents "recreate deletes and
// recreates it" — so GitLab was the only forge not honouring a contract the
// CLI already published. Fixed in the adapter; this scenario is the regression
// guard.
//
// The strategy flag is the configuration knob here, and it already existed:
// --strategy recreate is delete-then-create (this role), --strategy reconcile
// updates in place and never deletes the release object. Adding a second knob
// for the same choice would have been the wrong fix.
func TestRelease_ReReleasingATag_ReplacesRatherThanAccumulates(t *testing.T) {
	const (
		tag         = "v0.0.3-rc-live"
		firstBody   = "first body\n"
		secondBody  = "second body, which must replace the first\n"
		assetSuffix = "par-rel-3.txt"
	)

	for _, forge := range forgesClaiming(t, claimsReleaseAssets, "release assets") {
		t.Run(string(forge), func(t *testing.T) {
			// No t.Parallel: one lab is a single mutable fixture, and two
			// forges sharing it is not the property under test.
			target := livetest.Accept(t, forge)
			repo := livetest.NewScratchRepo(t, target, "release-rerelease")
			livetest.PrepareTag(t, target, repo, tag)

			adapter := livetest.Provider(t, target, repo)

			creator, ok := adapter.(provider.ReleaseCreator)
			if !ok {
				t.Fatalf("%s does not implement ReleaseCreator", forge)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()

			slug := livetest.RepoSlug(target, repo)
			asset := writeAsset(t, assetSuffix, "asset content\n")

			for _, round := range []struct {
				name string
				body string
			}{
				{name: "first", body: firstBody},
				{name: "second", body: secondBody},
			} {
				spec := provider.ReleaseSpec{
					Tag:       tag,
					Name:      "PAR-REL-3",
					NotesFile: writeNotes(t, round.body),
					Assets:    []string{asset},
				}

				if err := creator.CreateRelease(ctx, slug, spec); err != nil {
					t.Fatalf("%s CreateRelease (%s): %v", forge, round.name, err)
				}
			}

			release := mustReadRelease(ctx, t, target, repo, tag)

			if release.Body != secondBody {
				t.Errorf("%s body = %q, want the second release's %q — re-release did not replace",
					forge, release.Body, secondBody)
			}

			// The asset was supplied twice with the same name. One copy is the
			// answer; two means the forge accumulated, which turns a re-run
			// into a growing pile a consumer never asked for.
			names := release.AssetNames()
			if len(names) != 1 || names[0] != filepath.Base(asset) {
				t.Errorf("%s attached %v after two releases, want exactly [%s]",
					forge, names, filepath.Base(asset))
			}
		})
	}
}

func mustReadRelease(ctx context.Context, t *testing.T, target livetest.Target, repo, tag string) rawref.Release {
	t.Helper()

	reader := rawref.Reader{
		Forge:  string(target.Forge),
		Base:   target.BaseURL(),
		Token:  target.Token,
		Owner:  target.Owner,
		Client: livetest.HTTPClient(t, target, time.Minute),
	}

	release, found, err := rawref.ReleaseByTag(ctx, reader, repo, tag)
	if err != nil {
		t.Fatalf("%s raw release read: %v", target.Forge, err)
	}

	if !found {
		t.Fatalf("%s has no release at %s", target.Forge, tag)
	}

	return release
}

func writeAsset(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// assertAssetsArrivedIntact checks the attached set by name and then by the
// SHA-256 of the bytes the forge serves back. Names alone would pass for a
// forge that stored the right filenames over the wrong content.
func assertAssetsArrivedIntact(
	t *testing.T,
	forge provider.ForgeAPI,
	release rawref.Release,
	assets []string,
	digests map[string]string,
) {
	t.Helper()

	wantNames := make([]string, 0, len(assets))
	for _, path := range assets {
		wantNames = append(wantNames, filepath.Base(path))
	}

	slices.Sort(wantNames)

	if got := release.AssetNames(); !slices.Equal(got, wantNames) {
		t.Fatalf("%s attached %v, want %v", forge, got, wantNames)
	}

	for name, wantDigest := range digests {
		asset, ok := release.Asset(name)
		if !ok {
			t.Errorf("%s is missing asset %q", forge, name)

			continue
		}

		if asset.Digest != wantDigest {
			t.Errorf("%s asset %q digest = %s, want %s — the forge served different bytes than were uploaded",
				forge, name, asset.Digest, wantDigest)
		}
	}
}

// PAR-REL-4: `release publish --strategy reconcile` converges the release's
// assets on exactly the set it was given.
//
// PAR-REL-3 covers the other strategy: recreate deletes and re-creates, so its
// asset set is whatever the last call uploaded and nothing survives to be
// reconciled. Reconcile is the default, and it is the harder promise — the
// release keeps its identity while its assets are brought to match, which means
// an asset that is no longer wanted has to be actively removed.
//
// Removal is the half nothing was testing and the half most likely to be wrong,
// because the two forges keep assets in different places and delete them through
// different calls: GitLab attaches links to the release (deleteStaleReleaseLinks),
// the Gitea family uploads attachments. A reconcile that only ever adds looks
// perfect on a first release and silently accumulates stale artifacts forever
// after — which on a release page means shipping the previous version's binaries
// beside the current ones.
//
// So the scenario publishes twice with an overlapping set and asserts all three
// outcomes at once: kept, added, and gone.
func TestRelease_PublishReconcile_ConvergesOnTheGivenAssets(t *testing.T) {
	const tag = "v0.0.1-reconcile"

	for _, forge := range forgesClaiming(t, alwaysValidatesTokens, "releases") {
		t.Run(string(forge), func(t *testing.T) {
			target := livetest.Accept(t, forge)
			repo := livetest.NewScratchRepo(t, target, "relreconcile")

			livetest.PrepareTag(t, target, repo, tag)

			// One directory holding every input, and the CLI runs inside it.
			// The product requires relative paths for notes and assets — an
			// absolute one is refused as unsafe — so the scenario supplies bare
			// filenames rather than working around a rule that is deliberate.
			dir := t.TempDir()
			for name, body := range map[string]string{
				"first.txt":  "first\n",
				"shared.txt": "shared\n",
				"second.txt": "second\n",
				"notes.md":   "PAR-REL-4\n",
			} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			publish := func(assets ...string) livetest.Run {
				args := make([]string, 0, 11+2*len(assets))
				args = append(args,
					"release", "publish", "--strategy", "reconcile",
					"--tag", tag,
					"--repository", livetest.RepoSlug(target, repo),
					"--release-notes-file", "notes.md",
					"--draft=false",
				)
				for _, asset := range assets {
					args = append(args, "--asset", asset)
				}

				return livetest.CLIIn(t, target, repo, livetest.RunOptions{Dir: dir}, args...)
			}

			if run := publish("first.txt", "shared.txt"); run.ExitCode != 0 {
				t.Fatalf("%s: first publish failed (exit %d)\nstderr: %s", forge, run.ExitCode, run.Stderr)
			}

			// Non-vacuity: if the first publish attached nothing, the comparison
			// below would pass by having nothing to remove.
			got := livetest.ReleaseAssetNames(t, target, repo, tag)
			if !slices.Equal(got, []string{"first.txt", "shared.txt"}) {
				t.Fatalf("%s: after the first publish the release has %v, want [first.txt shared.txt]", forge, got)
			}

			if run := publish("shared.txt", "second.txt"); run.ExitCode != 0 {
				t.Fatalf("%s: second publish failed (exit %d)\nstderr: %s", forge, run.ExitCode, run.Stderr)
			}

			// The claim. first.txt must be gone, second.txt must have arrived, and
			// shared.txt must have survived without being duplicated.
			got = livetest.ReleaseAssetNames(t, target, repo, tag)
			if want := []string{"second.txt", "shared.txt"}; !slices.Equal(got, want) {
				t.Errorf("%s: reconcile left the release with %v, want %v — a stale asset that is never removed means a release page keeps shipping the previous version's files",
					forge, got, want)
			}
		})
	}
}

// PAR-REL-5: a large asset survives the upload path intact.
//
// §1.1 of the plan names this as the reason role presence is not proof: that
// `p.(ReleaseAssetUploader)` is satisfied says the method exists, not that
// GitLab accepts the multipart upload we send, that Forgejo returns the asset
// where we look for it, "or that either survives a 40 MB file". Every other
// release scenario uploads a few dozen bytes, which exercises none of the
// machinery that a real artifact does.
//
// Size is where upload paths actually differ. A few bytes fit in one buffer and
// one request on any forge; tens of megabytes is where streaming versus
// buffering, timeouts, and multipart boundaries start to matter, and where a
// forge's own limits live. The failure mode this guards is silent truncation —
// a release whose binary is the right name and the wrong length, which nobody
// notices until someone downloads it.
//
// The payload is pseudo-random rather than repeated bytes on purpose: a
// compressible payload can hide a truncation anywhere in the stack that
// re-encodes it, and random bytes make the digest sensitive to every byte.
// Deterministic seed, so a failure is reproducible.
func TestRelease_LargeAsset_ArrivesIntact(t *testing.T) {
	const (
		tag  = "v0.0.1-largeasset"
		name = "large-asset.bin"
		size = 40 << 20 // the 40 MB §1.1 asks about
	)

	for _, forge := range forgesClaiming(t, alwaysValidatesTokens, "releases") {
		t.Run(string(forge), func(t *testing.T) {
			target := livetest.Accept(t, forge)
			repo := livetest.NewScratchRepo(t, target, "largeasset")

			livetest.PrepareTag(t, target, repo, tag)

			dir := t.TempDir()
			writeLargeAsset(t, filepath.Join(dir, name), size)

			if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("PAR-REL-5\n"), 0o600); err != nil {
				t.Fatal(err)
			}

			wantDigest, wantSize, err := rawref.SHA256(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}

			if wantSize != size {
				t.Fatalf("fixture wrote %d bytes, want %d", wantSize, size)
			}

			run := livetest.CLIIn(t, target, repo, livetest.RunOptions{Dir: dir},
				"release", "publish", "--strategy", "reconcile",
				"--tag", tag,
				"--repository", livetest.RepoSlug(target, repo),
				"--release-notes-file", "notes.md",
				"--draft=false",
				"--asset", name,
			)
			if run.ExitCode != 0 {
				t.Fatalf("%s: publishing a %d-byte asset failed (exit %d)\nstderr: %s",
					forge, size, run.ExitCode, run.Stderr)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()

			release := mustReadRelease(ctx, t, target, repo, tag)

			asset, found := release.Asset(name)
			if !found {
				t.Fatalf("%s: the release has no asset %q (has %v)", forge, name, release.AssetNames())
			}

			// Length first: it names the failure. A truncation shows up in the
			// digest too, but "got 8 MiB of 40 MiB" is the sentence a maintainer
			// can act on, where a digest mismatch alone is not.
			if asset.Size != wantSize {
				t.Errorf("%s: asset is %d bytes, want %d — the upload was truncated",
					forge, asset.Size, wantSize)
			}

			// And the bytes themselves, since the right length carrying the wrong
			// content is the failure a length check cannot see.
			if asset.Digest != wantDigest {
				t.Errorf("%s: asset digest = %s, want %s — the forge served different bytes than were uploaded",
					forge, asset.Digest, wantDigest)
			}
		})
	}
}

// writeLargeAsset streams size bytes of pseudo-random data to path, without
// holding it in memory.
func writeLargeAsset(t *testing.T, path string, size int64) {
	t.Helper()

	file, err := os.Create(path) //nolint:gosec // path is this scenario's own t.TempDir().
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}()

	// Fixed seed: a failing run must be reproducible. Not a credential and not a
	// security decision, so a deterministic generator is the right one.
	source := rand.New(rand.NewPCG(0x5EED, 0x11FE)) //nolint:gosec // test payload, deliberately reproducible.

	// Filled a word at a time rather than a byte at a time: no narrowing
	// conversion to justify to a linter, and eight times fewer calls.
	buf := make([]byte, 1<<20)
	for written := int64(0); written < size; {
		chunk := int64(len(buf))
		if remaining := size - written; remaining < chunk {
			chunk = remaining
		}

		for i := 0; i+8 <= len(buf); i += 8 {
			binary.LittleEndian.PutUint64(buf[i:], source.Uint64())
		}

		n, writeErr := file.Write(buf[:chunk])
		if writeErr != nil {
			t.Fatal(writeErr)
		}

		written += int64(n)
	}
}
