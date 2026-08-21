// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package ociregistry

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// TestAllLoopback pins the security-relevant predicate: plain-HTTP (insecure)
// is enabled ONLY when every ref targets a loopback host. A real registry —
// or a mix — must stay on HTTPS so credentials never travel in clear text.
func TestAllLoopback(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		refs []string
		want bool
	}{
		{"localhost", []string{"localhost:5000/o/r:tag"}, true},
		{"loopback_ip", []string{"127.0.0.1:5000/o/r@sha256:" + strings.Repeat("a", 64)}, true},
		{"real_registry", []string{"ghcr.io/o/r:tag"}, false},
		{"mixed_loopback_and_real", []string{"127.0.0.1:5000/o/r:t", "ghcr.io/o/r:t"}, false},
		{"empty", nil, false},
		{"unparseable", []string{"::not a ref::"}, false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := allLoopback(testCase.refs); got != testCase.want {
				t.Errorf("allLoopback(%v) = %v, want %v", testCase.refs, got, testCase.want)
			}
		})
	}
}

// TestResolveDigestAndCopyTag round-trips the adapter against an in-process
// registry: a seeded image's digest resolves, and CopyTag points a second tag
// at the same digest — daemonless, no docker.
func TestResolveDigestAndCopyTag(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)

	repo := strings.TrimPrefix(srv.URL, "http://") + "/o/r"
	src := repo + ":src"

	img, err := random.Image(512, 1)
	if err != nil {
		t.Fatalf("random image: %v", err)
	}

	ref, err := name.ParseReference(src, name.Insecure)
	if err != nil {
		t.Fatal(err)
	}

	if err = remote.Write(ref, img, remote.WithContext(t.Context())); err != nil {
		t.Fatalf("seed image: %v", err)
	}

	want, err := img.Digest()
	if err != nil {
		t.Fatal(err)
	}

	adapter := New()

	got, err := adapter.ResolveDigest(t.Context(), src)
	if err != nil {
		t.Fatalf("ResolveDigest: %v", err)
	}

	if got != want.String() {
		t.Errorf("ResolveDigest = %s, want %s", got, want)
	}

	dest := repo + ":dest"
	if err = adapter.CopyTag(t.Context(), src, dest); err != nil {
		t.Fatalf("CopyTag: %v", err)
	}

	gotDest, err := adapter.ResolveDigest(t.Context(), dest)
	if err != nil {
		t.Fatalf("ResolveDigest(dest): %v", err)
	}

	if gotDest != want.String() {
		t.Errorf("after CopyTag, dest digest = %s, want %s", gotDest, want)
	}
}

// TestMergeManifest assembles a multi-platform index from two per-arch images
// and asserts the written index advertises both children with the correct
// platforms — the daemonless replacement for `docker buildx imagetools create`.
func TestMergeManifest(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)

	repo := strings.TrimPrefix(srv.URL, "http://") + "/o/r"

	platforms := []struct {
		os, arch, hex string
	}{
		{os: "linux", arch: "amd64"},
		{os: "linux", arch: "arm64"},
	}

	for i := range platforms {
		img, err := random.Image(512, 1)
		if err != nil {
			t.Fatalf("random image: %v", err)
		}

		config, err := img.ConfigFile()
		if err != nil {
			t.Fatal(err)
		}

		config = config.DeepCopy()
		config.OS = platforms[i].os
		config.Architecture = platforms[i].arch

		img, err = mutate.ConfigFile(img, config)
		if err != nil {
			t.Fatalf("set platform: %v", err)
		}

		ref, err := name.ParseReference(repo+":"+platforms[i].arch, name.Insecure)
		if err != nil {
			t.Fatal(err)
		}

		if err = remote.Write(ref, img, remote.WithContext(t.Context())); err != nil {
			t.Fatalf("seed %s: %v", platforms[i].arch, err)
		}

		digest, err := img.Digest()
		if err != nil {
			t.Fatal(err)
		}

		platforms[i].hex = digest.Hex
	}

	tag := repo + ":multi"
	if err := New().MergeManifest(t.Context(), repo, []string{platforms[0].hex, platforms[1].hex}, []string{tag}); err != nil {
		t.Fatalf("MergeManifest: %v", err)
	}

	tagRef, err := name.ParseReference(tag, name.Insecure)
	if err != nil {
		t.Fatal(err)
	}

	index, err := remote.Index(tagRef, remote.WithContext(t.Context()))
	if err != nil {
		t.Fatalf("fetch index: %v", err)
	}

	manifest, err := index.IndexManifest()
	if err != nil {
		t.Fatal(err)
	}

	if len(manifest.Manifests) != 2 {
		t.Fatalf("index has %d manifests, want 2", len(manifest.Manifests))
	}

	got := map[string]string{}
	for _, child := range manifest.Manifests {
		if child.Platform == nil {
			t.Fatalf("child %s has no platform descriptor", child.Digest)
		}

		got[child.Digest.Hex] = child.Platform.OS + "/" + child.Platform.Architecture
	}

	for _, want := range platforms {
		if got[want.hex] != want.os+"/"+want.arch {
			t.Errorf("child %s platform = %q, want %s/%s", want.hex, got[want.hex], want.os, want.arch)
		}
	}
}

// TestManifest fetches the raw manifest the registry serves and checks it is
// the real document (valid JSON referencing the image's config digest).
func TestManifest(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)

	repo := strings.TrimPrefix(srv.URL, "http://") + "/o/r"

	img, err := random.Image(256, 1)
	if err != nil {
		t.Fatalf("random image: %v", err)
	}

	ref, err := name.ParseReference(repo+":x", name.Insecure)
	if err != nil {
		t.Fatal(err)
	}

	if err = remote.Write(ref, img, remote.WithContext(t.Context())); err != nil {
		t.Fatalf("seed image: %v", err)
	}

	raw, err := New().Manifest(t.Context(), repo+":x")
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}

	if !json.Valid(raw) {
		t.Fatalf("manifest is not valid JSON: %s", raw)
	}

	config, err := img.ConfigName()
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(raw), config.Hex) {
		t.Errorf("manifest does not reference config digest %s:\n%s", config.Hex, raw)
	}
}

// TestMergeManifestFlattensPerArchIndexes covers the shape buildx actually
// pushes when attestations are on: each per-arch build lands as a
// single-platform *index* (image + attestation manifest), not an image
// manifest. Merging must flatten those into the release index — keeping the
// attestation children and their vnd.docker.reference.* annotations — instead
// of resolving the default platform, which used to fail an arm64-only child
// with "no child with platform linux/amd64".
func TestMergeManifestFlattensPerArchIndexes(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)

	repo := strings.TrimPrefix(srv.URL, "http://") + "/o/r"

	type child struct {
		digest   string
		platform string
	}

	var (
		pushed   []string
		expected []child
	)

	for _, arch := range []string{"amd64", "arm64"} {
		img := platformImage(t, "linux", arch)

		attestation, err := random.Image(128, 1)
		if err != nil {
			t.Fatalf("random attestation: %v", err)
		}

		imgDigest, err := img.Digest()
		if err != nil {
			t.Fatal(err)
		}

		index := mutate.AppendManifests(empty.Index,
			mutate.IndexAddendum{
				Add:        img,
				Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: arch}},
			},
			mutate.IndexAddendum{
				Add: attestation,
				Descriptor: v1.Descriptor{
					Platform: &v1.Platform{OS: "unknown", Architecture: "unknown"},
					Annotations: map[string]string{
						"vnd.docker.reference.type":   "attestation-manifest",
						"vnd.docker.reference.digest": imgDigest.String(),
					},
				},
			},
		)

		indexDigest, err := index.Digest()
		if err != nil {
			t.Fatal(err)
		}

		ref, err := name.ParseReference(repo+"@"+indexDigest.String(), name.Insecure)
		if err != nil {
			t.Fatal(err)
		}

		if err = remote.WriteIndex(ref, index, remote.WithContext(t.Context())); err != nil {
			t.Fatalf("seed %s index: %v", arch, err)
		}

		attestationDigest, err := attestation.Digest()
		if err != nil {
			t.Fatal(err)
		}

		pushed = append(pushed, indexDigest.Hex)
		expected = append(expected,
			child{digest: imgDigest.Hex, platform: "linux/" + arch},
			child{digest: attestationDigest.Hex, platform: "unknown/unknown"},
		)
	}

	tag := repo + ":multi"
	if err := New().MergeManifest(t.Context(), repo, pushed, []string{tag}); err != nil {
		t.Fatalf("MergeManifest: %v", err)
	}

	manifest := fetchIndexManifest(t, tag)

	if len(manifest.Manifests) != len(expected) {
		t.Fatalf("index has %d manifests, want %d", len(manifest.Manifests), len(expected))
	}

	got := map[string]v1.Descriptor{}
	for _, descriptor := range manifest.Manifests {
		got[descriptor.Digest.Hex] = descriptor
	}

	for _, want := range expected {
		descriptor, ok := got[want.digest]
		if !ok {
			t.Errorf("child %s missing from merged index", want.digest)

			continue
		}

		if descriptor.Platform == nil {
			t.Errorf("child %s has no platform descriptor", want.digest)

			continue
		}

		if plat := descriptor.Platform.OS + "/" + descriptor.Platform.Architecture; plat != want.platform {
			t.Errorf("child %s platform = %q, want %q", want.digest, plat, want.platform)
		}

		if want.platform != "unknown/unknown" {
			continue
		}

		if descriptor.Annotations["vnd.docker.reference.type"] != "attestation-manifest" {
			t.Errorf("attestation child %s lost its vnd.docker.reference.* annotations: %v", want.digest, descriptor.Annotations)
		}
	}
}

// platformImage returns a random image whose config advertises os/arch.
func platformImage(t *testing.T, osName, arch string) v1.Image {
	t.Helper()

	img, err := random.Image(512, 1)
	if err != nil {
		t.Fatalf("random image: %v", err)
	}

	config, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}

	config = config.DeepCopy()
	config.OS = osName
	config.Architecture = arch

	img, err = mutate.ConfigFile(img, config)
	if err != nil {
		t.Fatalf("set platform: %v", err)
	}

	return img
}

// fetchIndexManifest reads back the index a tag serves.
func fetchIndexManifest(t *testing.T, tag string) *v1.IndexManifest {
	t.Helper()

	ref, err := name.ParseReference(tag, name.Insecure)
	if err != nil {
		t.Fatal(err)
	}

	index, err := remote.Index(ref, remote.WithContext(t.Context()))
	if err != nil {
		t.Fatalf("fetch index: %v", err)
	}

	manifest, err := index.IndexManifest()
	if err != nil {
		t.Fatal(err)
	}

	return manifest
}
