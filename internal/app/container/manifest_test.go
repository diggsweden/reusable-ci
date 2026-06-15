// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/internal/app/container"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

// fakeManifestRegistry stubs the daemonless registry surface the manifest
// helpers depend on, recording calls so the app-layer logic (digest
// validation, output emission, index assembly inputs) can be asserted without
// a real registry. The real round-trip lives in the ociregistry adapter test.
type fakeManifestRegistry struct {
	digest      string
	digestErr   error
	manifest    []byte
	manifestErr error
	mergeErr    error

	resolveCalls  []string
	manifestCalls []string

	mergeCalls    int
	mergedImage   string
	mergedDigests []string
	mergedTags    []string
}

func (f *fakeManifestRegistry) ResolveDigest(_ context.Context, ref string) (string, error) {
	f.resolveCalls = append(f.resolveCalls, ref)

	return f.digest, f.digestErr
}

func (f *fakeManifestRegistry) Manifest(_ context.Context, ref string) ([]byte, error) {
	f.manifestCalls = append(f.manifestCalls, ref)

	if f.manifestErr != nil {
		return nil, f.manifestErr
	}

	if f.manifest != nil {
		return f.manifest, nil
	}

	return []byte("manifest output"), nil
}

func (f *fakeManifestRegistry) MergeManifest(_ context.Context, image string, digests, tags []string) error {
	f.mergeCalls++
	f.mergedImage = image
	f.mergedDigests = digests
	f.mergedTags = tags

	return f.mergeErr
}

const oneDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

func TestInspectManifest_UsesExplicitImage(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	reg := &fakeManifestRegistry{digest: oneDigest, manifest: []byte("manifest output")}

	var out bytes.Buffer

	res, err := appcontainer.InspectManifest(context.Background(), reg, sink, &out, appcontainer.InspectManifestInput{Image: "ghcr.io/org/app:v1"}) //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	if err != nil {
		t.Fatal(err)
	}

	if res.Image != "ghcr.io/org/app:v1" || res.Digest != oneDigest {
		t.Fatalf("res = %+v", res)
	}

	if got := sink.Single("image"); got != "ghcr.io/org/app:v1" {
		t.Errorf("image output = %q", got)
	}

	if got := sink.Single("digest"); got != oneDigest {
		t.Errorf("digest output = %q", got)
	}

	// image-digest-ref must be the cosign-ready immutable form: the bare image
	// (tag stripped) joined to the digest with `@`.
	if got := sink.Single("image-digest-ref"); got != "ghcr.io/org/app@"+oneDigest {
		t.Errorf("image-digest-ref output = %q, want ghcr.io/org/app@%s", got, oneDigest)
	}

	if !strings.Contains(out.String(), "manifest output") {
		t.Errorf("out = %q (want the fetched manifest printed for the run log)", out.String())
	}

	assertInspected(t, reg, "ghcr.io/org/app:v1")
}

func TestInspectManifest_RejectsMalformedDigest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		digest string
	}{
		{"empty", ""},
		{"missing sha256 prefix", "abc1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcd"},
		{"wrong algorithm", "sha512:abc1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcd"},
		{"too short", "sha256:abc123"},
		{"uppercase hex", "sha256:ABC1234567890ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890ABCD"},
		{"trailing garbage", "sha256:abc1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcd extra"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			sink := fakeoutputsink.New(t)
			reg := &fakeManifestRegistry{digest: c.digest}

			_, err := appcontainer.InspectManifest(context.Background(), reg, sink, io.Discard, appcontainer.InspectManifestInput{Image: "ghcr.io/org/app:v1"})
			if err == nil {
				t.Fatalf("expected rejection for malformed digest %q, got nil error", c.digest)
			}

			if !strings.Contains(err.Error(), "unexpected digest format") {
				t.Errorf("error must point at digest format; got %v", err)
			}
		})
	}
}

func TestStripTag_HandlesPortInRegistry(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, want string
	}{
		{"ghcr.io/org/app:v1", "ghcr.io/org/app"},
		{"ghcr.io/org/app", "ghcr.io/org/app"},
		{"localhost:5000/org/app:v1", "localhost:5000/org/app"},
		{"localhost:5000/app", "localhost:5000/app"},
		{"alpine:3.21", "alpine"},
		{"alpine", "alpine"},
	}

	for _, c := range cases {
		// stripTag is package-private; exercise it via the public
		// InspectManifest output to avoid widening the API.
		sink := fakeoutputsink.New(t)
		reg := &fakeManifestRegistry{digest: oneDigest}

		_, err := appcontainer.InspectManifest(context.Background(), reg, sink, io.Discard, appcontainer.InspectManifestInput{Image: c.in})
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}

		if got := sink.Single("image-digest-ref"); got != c.want+"@"+oneDigest {
			t.Errorf("stripTag(%q) → image-digest-ref = %q, want %s@%s", c.in, got, c.want, oneDigest)
		}
	}
}

func TestInspectManifest_UsesFirstTagWhenImageEmpty(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	reg := &fakeManifestRegistry{digest: "sha256:2222222222222222222222222222222222222222222222222222222222222222"}

	_, err := appcontainer.InspectManifest(context.Background(), reg, sink, io.Discard, appcontainer.InspectManifestInput{Tags: "\n  ghcr.io/org/app:v1\n  ghcr.io/org/app:latest\n"})
	if err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("image"); got != "ghcr.io/org/app:v1" {
		t.Errorf("image output = %q", got)
	}

	assertInspected(t, reg, "ghcr.io/org/app:v1")
}

func TestInspectManifest_RequiresImageOrTags(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	_, err := appcontainer.InspectManifest(context.Background(), &fakeManifestRegistry{}, sink, io.Discard, appcontainer.InspectManifestInput{})
	if err == nil || !strings.Contains(err.Error(), "image or tags is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestInspectManifest_PropagatesManifestFetchFailure(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	mockErr := errors.New("registry unreachable") //nolint:err113 // test mock error.

	_, err := appcontainer.InspectManifest(context.Background(), &fakeManifestRegistry{manifestErr: mockErr}, sink, io.Discard, appcontainer.InspectManifestInput{Image: "img"}) //nolint:goconst // test fixture image name.
	if err == nil || !strings.Contains(err.Error(), "registry unreachable") {
		t.Fatalf("err = %v", err)
	}
}

func TestInspectManifest_PropagatesDigestResolveFailure(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	mockErr := errors.New("digest resolve failed") //nolint:err113 // test mock error.

	_, err := appcontainer.InspectManifest(context.Background(), &fakeManifestRegistry{digestErr: mockErr}, sink, io.Discard, appcontainer.InspectManifestInput{Image: "img"})
	if err == nil || !strings.Contains(err.Error(), "digest resolve failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestMergeManifest_PassesSortedDigestsAndTags(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.MkdirAll("digests")
	digestA := strings.Repeat("a", 64)
	digestB := strings.Repeat("b", 64)
	fsys.WriteFile("digests/"+digestB, nil)
	fsys.WriteFile("digests/"+digestA, nil)

	reg := &fakeManifestRegistry{}

	if err := appcontainer.MergeManifest(context.Background(), reg, io.Discard, appcontainer.MergeManifestInput{
		ImageName:  "ghcr.io/org/app",
		Tags:       "ghcr.io/org/app:v1\nghcr.io/org/app:latest\n",
		DigestsDir: dir,
	}); err != nil {
		t.Fatal(err)
	}

	if reg.mergeCalls != 1 {
		t.Fatalf("MergeManifest called %d times, want 1", reg.mergeCalls)
	}

	if reg.mergedImage != "ghcr.io/org/app" {
		t.Errorf("image = %q", reg.mergedImage)
	}

	if !reflect.DeepEqual(reg.mergedDigests, []string{digestA, digestB}) {
		t.Errorf("digests = %v, want sorted [a… b…]", reg.mergedDigests)
	}

	if !reflect.DeepEqual(reg.mergedTags, []string{"ghcr.io/org/app:v1", "ghcr.io/org/app:latest"}) {
		t.Errorf("tags = %v", reg.mergedTags)
	}
}

func TestMergeManifest_RejectsInvalidDigestMarker(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.MkdirAll("digests")
	fsys.WriteFile("digests/not-a-digest", nil)

	reg := &fakeManifestRegistry{}

	err := appcontainer.MergeManifest(context.Background(), reg, io.Discard, appcontainer.MergeManifestInput{
		ImageName:  "ghcr.io/org/app",
		Tags:       "ghcr.io/org/app:v1",
		DigestsDir: dir,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid digest marker") {
		t.Fatalf("err = %v", err)
	}

	if reg.mergeCalls != 0 {
		t.Errorf("registry must not be called when a digest marker is invalid")
	}
}

func TestMergeManifest_RequiresTags(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.MkdirAll("digests")
	fsys.WriteFile("digests/"+strings.Repeat("a", 64), nil)

	err := appcontainer.MergeManifest(context.Background(), &fakeManifestRegistry{}, io.Discard, appcontainer.MergeManifestInput{ImageName: "img", DigestsDir: dir})
	if err == nil || !strings.Contains(err.Error(), "tags is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestWriteDigestMarker_NormalizesDigest(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.MkdirAll("digests")
	digest := strings.Repeat("A", 64)

	path, err := appcontainer.WriteDigestMarker(appcontainer.WriteDigestMarkerInput{
		Digest:     "sha256:" + digest,
		DigestsDir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}

	want := fsys.Path("digests", strings.ToLower(digest))
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}

	if _, err := os.Stat(want); err != nil {
		t.Fatalf("digest marker missing: %v", err)
	}
}

func TestWriteDigestMarker_RejectsInvalidDigest(t *testing.T) {
	t.Parallel()

	_, err := appcontainer.WriteDigestMarker(appcontainer.WriteDigestMarkerInput{Digest: "sha256:nothex"})
	if err == nil || !strings.Contains(err.Error(), "64 hex chars") {
		t.Fatalf("err = %v", err)
	}
}

// assertInspected checks InspectManifest fetched the manifest and resolved the
// digest for exactly the expected image reference.
func assertInspected(t *testing.T, reg *fakeManifestRegistry, image string) {
	t.Helper()

	if !reflect.DeepEqual(reg.manifestCalls, []string{image}) {
		t.Errorf("Manifest calls = %v, want [%s]", reg.manifestCalls, image)
	}

	if !reflect.DeepEqual(reg.resolveCalls, []string{image}) {
		t.Errorf("ResolveDigest calls = %v, want [%s]", reg.resolveCalls, image)
	}
}
