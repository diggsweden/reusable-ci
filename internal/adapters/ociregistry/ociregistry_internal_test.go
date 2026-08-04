// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package ociregistry

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// TestPushLayoutByDigest writes a random image to an OCI layout, pushes it
// tagless by digest to an in-process registry, and asserts the manifest is
// retrievable at that exact digest with NO tag created — the daemonless
// push-by-digest the buildah build path relies on (no ephemeral tag to clean up).
func TestPushLayoutByDigest(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)

	repo := strings.TrimPrefix(srv.URL, "http://") + "/o/r"

	img, err := random.Image(256, 1)
	if err != nil {
		t.Fatalf("random image: %v", err)
	}

	layoutDir := t.TempDir()

	p, err := layout.Write(layoutDir, empty.Index)
	if err != nil {
		t.Fatalf("layout.Write: %v", err)
	}

	if err = p.AppendImage(img); err != nil {
		t.Fatalf("AppendImage: %v", err)
	}

	want, err := img.Digest()
	if err != nil {
		t.Fatal(err)
	}

	got, err := New().PushLayoutByDigest(t.Context(), layoutDir, repo)
	if err != nil {
		t.Fatalf("PushLayoutByDigest: %v", err)
	}

	if got != want.String() {
		t.Errorf("returned digest = %s, want %s", got, want)
	}

	dref, err := name.NewDigest(repo+"@"+got, name.Insecure)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = remote.Get(dref, remote.WithContext(t.Context())); err != nil {
		t.Fatalf("manifest not retrievable by digest: %v", err)
	}

	repoRef, err := name.NewRepository(repo, name.Insecure)
	if err != nil {
		t.Fatal(err)
	}

	tags, err := remote.List(repoRef, remote.WithContext(t.Context()))
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}

	if len(tags) != 0 {
		t.Errorf("push-by-digest created tags %v; want none (tagless)", tags)
	}
}

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
		{"unparsable", []string{"::not a ref::"}, false},
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

func TestResolveDigest_MissingTagIsMissingInput(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)

	repo := strings.TrimPrefix(srv.URL, "http://") + "/o/r"

	_, err := New().ResolveDigest(t.Context(), repo+":missing")
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Fatalf("ResolveDigest missing tag error = %v, want ErrMissingInput", err)
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
		{os: osLinux, arch: "amd64"},
		{os: osLinux, arch: "arm64"},
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

// seedBuildKitIndex writes a BuildKit-style source index for one arch — a
// platform image plus an unknown/unknown attestation manifest carrying
// vnd.docker.reference.* links — and returns the index digest hex. Extracted
// from the test so the test body stays readable (and within complexity budget).
func seedBuildKitIndex(t *testing.T, repo, arch string) string {
	t.Helper()

	img, err := random.Image(512, 1)
	if err != nil {
		t.Fatalf("random image: %v", err)
	}

	cfg, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}

	cfg = cfg.DeepCopy()
	cfg.OS, cfg.Architecture = osLinux, arch

	img, err = mutate.ConfigFile(img, cfg)
	if err != nil {
		t.Fatalf("set platform: %v", err)
	}

	imgDigest, err := img.Digest()
	if err != nil {
		t.Fatal(err)
	}

	// Stand-in for a BuildKit provenance/SBOM attestation manifest.
	att, err := random.Image(256, 1)
	if err != nil {
		t.Fatalf("random attestation: %v", err)
	}

	idx := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{Add: img, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: osLinux, Architecture: arch}}},
		mutate.IndexAddendum{Add: att, Descriptor: v1.Descriptor{
			Platform: &v1.Platform{OS: "unknown", Architecture: "unknown"},
			Annotations: map[string]string{
				"vnd.docker.reference.type":   "attestation-manifest",
				"vnd.docker.reference.digest": imgDigest.String(),
			},
		}},
	)

	ref, err := name.ParseReference(repo+":src-"+arch, name.Insecure)
	if err != nil {
		t.Fatal(err)
	}

	if err = remote.WriteIndex(ref, idx, remote.WithContext(t.Context())); err != nil {
		t.Fatalf("seed index %s: %v", arch, err)
	}

	idxDigest, err := idx.Digest()
	if err != nil {
		t.Fatal(err)
	}

	return idxDigest.Hex
}

// TestMergeManifest_CarriesAttestations exercises the attestation-aware path:
// each per-arch source is a BuildKit-style index (platform image + an
// unknown/unknown attestation manifest carrying vnd.docker.reference.* links).
// The merged multi-arch index must contain every child — both platform images
// AND both attestation manifests, annotations preserved — so SLSA provenance /
// SBOM survive the merge without a separate attestation API.
func TestMergeManifest_CarriesAttestations(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)

	repo := strings.TrimPrefix(srv.URL, "http://") + "/o/r"

	srcHex := []string{
		seedBuildKitIndex(t, repo, "amd64"),
		seedBuildKitIndex(t, repo, "arm64"),
	}

	tag := repo + ":multi"
	if err := New().MergeManifest(t.Context(), repo, srcHex, []string{tag}); err != nil {
		t.Fatalf("MergeManifest: %v", err)
	}

	tagRef, err := name.ParseReference(tag, name.Insecure)
	if err != nil {
		t.Fatal(err)
	}

	index, err := remote.Index(tagRef, remote.WithContext(t.Context()))
	if err != nil {
		t.Fatalf("fetch merged index: %v", err)
	}

	manifest, err := index.IndexManifest()
	if err != nil {
		t.Fatal(err)
	}

	if len(manifest.Manifests) != 4 {
		t.Fatalf("merged index has %d manifests, want 4 (2 images + 2 attestations)", len(manifest.Manifests))
	}

	platforms := map[string]bool{}
	attestations := 0

	for _, child := range manifest.Manifests {
		if child.Annotations["vnd.docker.reference.type"] == "attestation-manifest" {
			attestations++

			if child.Annotations["vnd.docker.reference.digest"] == "" {
				t.Errorf("attestation manifest %s lost its reference.digest annotation", child.Digest)
			}

			continue
		}

		if child.Platform != nil {
			platforms[child.Platform.OS+"/"+child.Platform.Architecture] = true
		}
	}

	if attestations != 2 {
		t.Errorf("merged index carried %d attestation manifests, want 2", attestations)
	}

	if !platforms["linux/amd64"] || !platforms["linux/arm64"] {
		t.Errorf("merged index missing platform images; got %v", platforms)
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

func TestLabelsFetchesImageConfigLabels(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)

	repo := strings.TrimPrefix(srv.URL, "http://") + "/o/r"

	img, err := random.Image(256, 1)
	if err != nil {
		t.Fatalf("random image: %v", err)
	}

	config, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}

	config = config.DeepCopy()
	config.Config.Labels = map[string]string{
		"org.opencontainers.image.revision": "abc123",
		"org.opencontainers.image.version":  "v1.2.3",
	}

	img, err = mutate.ConfigFile(img, config)
	if err != nil {
		t.Fatalf("set labels: %v", err)
	}

	ref, err := name.ParseReference(repo+":x", name.Insecure)
	if err != nil {
		t.Fatal(err)
	}

	if err = remote.Write(ref, img, remote.WithContext(t.Context())); err != nil {
		t.Fatalf("seed image: %v", err)
	}

	labels, err := New().Labels(t.Context(), repo+":x")
	if err != nil {
		t.Fatalf("Labels: %v", err)
	}

	if labels["org.opencontainers.image.revision"] != "abc123" || labels["org.opencontainers.image.version"] != "v1.2.3" {
		t.Fatalf("labels = %v", labels)
	}
}

// A source that does not exist is a permanent condition, and the exit ladder
// must say so. Reported as "the dependency is unavailable" it becomes exit 69,
// which tells CI to retry something that can never succeed — the failure mode
// that hid a real defect: on a forge that drops untagged manifests, rolling back
// a moving tag retried forever instead of reporting the image was gone.
func TestCopyTag_MissingSourceIsMissingInputNotUnavailable(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)

	repo := strings.TrimPrefix(srv.URL, "http://") + "/o/r"

	err := New().CopyTag(t.Context(), repo+":absent", repo+":dest")
	if err == nil {
		t.Fatal("CopyTag from a missing source returned no error")
	}

	if errors.Is(err, errs.ErrDependencyUnavailable) {
		t.Errorf("CopyTag classified a missing source as the dependency being unavailable, which tells CI to retry: %v", err)
	}

	if !errors.Is(err, errs.ErrMissingInput) {
		t.Errorf("CopyTag missing-source error = %v, want ErrMissingInput", err)
	}
}
