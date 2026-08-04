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
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
	"github.com/diggsweden/reusable-ci/v3/internal/livetest/rawref"
)

// forEachForgeClaiming runs one scenario against every live forge whose
// Capabilities() claims what it needs, and says out loud which forges were
// skipped and why.
//
// This is the shape that keeps the tier a parity suite rather than N suites
// that drift: a scenario is a single body, and the capability decides where it
// runs. A silent skip would be worse than no test, so an unclaimed capability
// is reported, not swallowed.
func forEachForgeClaiming(
	t *testing.T,
	needs func(provider.Capabilities) bool,
	capability string,
	run func(t *testing.T, target livetest.Target),
) {
	t.Helper()

	ran := 0

	for _, kind := range livetest.LiveForges() {
		capabilities, known := livetest.Capabilities(kind)
		if !known {
			t.Fatalf("no adapter for platform %q", kind)
		}

		if !needs(capabilities) {
			t.Logf("SKIP %s: does not claim %s", kind, capability)

			continue
		}

		ran++

		t.Run(string(kind), func(t *testing.T) {
			// No t.Parallel: one lab is a single mutable fixture, and two
			// forges sharing it is not the property under test.
			run(t, livetest.Accept(t, kind))
		})
	}

	if ran == 0 {
		t.Fatalf("no live forge claims %s, so this scenario proved nothing", capability)
	}
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

	forEachForgeClaiming(t, claimsReleaseAssets, "release assets",
		func(t *testing.T, target livetest.Target) {
			kind := target.Kind
			repo := livetest.NewScratchRepo(t, target, "release-assets")

			// Both adapters release an existing tag rather than creating one,
			// so the fixture has to supply the commit and the tag first.
			livetest.PrepareTag(t, target, repo, tag)

			assets, digests := writeAssets(t)

			forge := livetest.Provider(t, target, repo)

			creator, ok := forge.(provider.ReleaseCreator)
			if !ok {
				t.Fatalf("%s does not implement ReleaseCreator", kind)
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
				t.Fatalf("%s CreateRelease: %v", kind, err)
			}

			// Every assertion below reads the forge directly. Going through the
			// adapter would let an adapter that mis-parses its own writes agree
			// with itself and pass.
			reader := rawref.Reader{
				Forge: string(kind),
				Base:  target.BaseURL(),
				Token: target.Token,
				Owner: target.Owner,
			}

			release, found, err := rawref.ReleaseByTag(ctx, reader, repo, tag)
			if err != nil {
				t.Fatalf("%s raw release read: %v", kind, err)
			}

			if !found {
				t.Fatalf("%s reported success but has no release at %s", kind, tag)
			}

			if release.Tag != tag {
				t.Errorf("%s release tag = %q, want %q", kind, release.Tag, tag)
			}

			if release.Name != spec.Name {
				t.Errorf("%s release name = %q, want %q", kind, release.Name, spec.Name)
			}

			if release.Body != body {
				t.Errorf("%s release body = %q, want %q", kind, release.Body, body)
			}

			wantNames := make([]string, 0, len(assets))
			for _, path := range assets {
				wantNames = append(wantNames, filepath.Base(path))
			}

			slices.Sort(wantNames)

			if got := release.AssetNames(); !slices.Equal(got, wantNames) {
				t.Fatalf("%s attached %v, want %v", kind, got, wantNames)
			}

			for name, wantDigest := range digests {
				asset, ok := release.Asset(name)
				if !ok {
					t.Errorf("%s is missing asset %q", kind, name)

					continue
				}

				if asset.Digest != wantDigest {
					t.Errorf("%s asset %q digest = %s, want %s — the forge served different bytes than were uploaded",
						kind, name, asset.Digest, wantDigest)
				}
			}
		})
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

	forEachForgeClaiming(t, claimsReleaseAssets, "release assets",
		func(t *testing.T, target livetest.Target) {
			kind := target.Kind
			repo := livetest.NewScratchRepo(t, target, "release-upload")
			livetest.PrepareTag(t, target, repo, tag)

			forge := livetest.Provider(t, target, repo)

			creator, ok := forge.(provider.ReleaseCreator)
			if !ok {
				t.Fatalf("%s does not implement ReleaseCreator", kind)
			}

			uploader, ok := forge.(provider.ReleaseAssetUploader)
			if !ok {
				t.Fatalf("%s claims release assets but does not implement ReleaseAssetUploader", kind)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()

			if err := creator.CreateRelease(ctx, livetest.RepoSlug(target, repo),
				provider.ReleaseSpec{Tag: tag, Name: "PAR-REL-2"}); err != nil {
				t.Fatalf("%s CreateRelease: %v", kind, err)
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
				t.Fatalf("%s UploadReleaseAsset: %v", kind, err)
			}

			release := mustReadRelease(ctx, t, target, repo, tag)

			asset, found := release.Asset(filepath.Base(path))
			if !found {
				t.Fatalf("%s accepted the upload but serves no asset %q (has %v)",
					kind, filepath.Base(path), release.AssetNames())
			}

			if asset.Digest != wantDigest {
				t.Errorf("%s asset digest = %s, want %s — the forge served different bytes than were uploaded",
					kind, asset.Digest, wantDigest)
			}

			if asset.Size != wantSize {
				t.Errorf("%s asset size = %s, want %s", kind,
					rawref.FormatSize(asset.Size), rawref.FormatSize(wantSize))
			}
		})
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

	forEachForgeClaiming(t, claimsReleaseAssets, "release assets",
		func(t *testing.T, target livetest.Target) {
			kind := target.Kind
			repo := livetest.NewScratchRepo(t, target, "release-rerelease")
			livetest.PrepareTag(t, target, repo, tag)

			forge := livetest.Provider(t, target, repo)

			creator, ok := forge.(provider.ReleaseCreator)
			if !ok {
				t.Fatalf("%s does not implement ReleaseCreator", kind)
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
					t.Fatalf("%s CreateRelease (%s): %v", kind, round.name, err)
				}
			}

			release := mustReadRelease(ctx, t, target, repo, tag)

			if release.Body != secondBody {
				t.Errorf("%s body = %q, want the second release's %q — re-release did not replace",
					kind, release.Body, secondBody)
			}

			// The asset was supplied twice with the same name. One copy is the
			// answer; two means the forge accumulated, which turns a re-run
			// into a growing pile a consumer never asked for.
			names := release.AssetNames()
			if len(names) != 1 || names[0] != filepath.Base(asset) {
				t.Errorf("%s attached %v after two releases, want exactly [%s]",
					kind, names, filepath.Base(asset))
			}
		})
}

func mustReadRelease(ctx context.Context, t *testing.T, target livetest.Target, repo, tag string) rawref.Release {
	t.Helper()

	reader := rawref.Reader{
		Forge: string(target.Kind),
		Base:  target.BaseURL(),
		Token: target.Token,
		Owner: target.Owner,
	}

	release, found, err := rawref.ReleaseByTag(ctx, reader, repo, tag)
	if err != nil {
		t.Fatalf("%s raw release read: %v", target.Kind, err)
	}

	if !found {
		t.Fatalf("%s has no release at %s", target.Kind, tag)
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
