// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
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

	// events interleaves every call in order, which the per-method fields
	// cannot show.
	events []string
}

func (f *fakeManifestRegistry) ResolveDigest(_ context.Context, ref string) (string, error) {
	f.resolveCalls = append(f.resolveCalls, ref)
	f.events = append(f.events, "resolve:"+ref)

	return f.digest, f.digestErr
}

func (f *fakeManifestRegistry) Manifest(_ context.Context, ref string) ([]byte, error) {
	f.manifestCalls = append(f.manifestCalls, ref)
	f.events = append(f.events, "manifest:"+ref)

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
	f.events = append(f.events, "merge:"+image)
	f.mergedImage = image
	f.mergedDigests = digests
	f.mergedTags = tags

	return f.mergeErr
}

// Named sentinels rather than inline errors.New: the propagation tests
// below assert with errors.Is, so what survives the wrap is the identity
// and not a substring of a message.
var (
	errRegistryUnreachable = errors.New("registry unreachable")
	errDigestResolveFailed = errors.New("digest resolve failed")
)

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

			// ErrInvalidConfig: the registry answered, and what it said is
			// unusable — not the caller's mistake and not a missing image.
			if !errors.Is(err, errs.ErrInvalidConfig) {
				t.Fatalf("digest %q: err = %v, want ErrInvalidConfig", c.digest, err)
			}

			if !strings.Contains(err.Error(), "unexpected digest format") {
				t.Errorf("error must point at digest format; got %v", err)
			}

			if got := sink.Keys(); len(got) != 0 {
				t.Errorf("emitted %q for an unusable digest", got)
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
	if !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), "image or tags is required") {
		t.Fatalf("err = %v, want ErrUsage naming the missing selector", err)
	}
}

func TestInspectManifest_PropagatesManifestFetchFailure(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	_, err := appcontainer.InspectManifest(context.Background(), &fakeManifestRegistry{manifestErr: errRegistryUnreachable}, sink, io.Discard, appcontainer.InspectManifestInput{Image: "img"}) //nolint:goconst // test fixture image name.
	if !errors.Is(err, errRegistryUnreachable) {
		t.Fatalf("err = %v, want the registry's own error to survive wrapping", err)
	}
}

func TestInspectManifest_PropagatesDigestResolveFailure(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	_, err := appcontainer.InspectManifest(context.Background(), &fakeManifestRegistry{digestErr: errDigestResolveFailed}, sink, io.Discard, appcontainer.InspectManifestInput{Image: "img"})
	if !errors.Is(err, errDigestResolveFailed) {
		t.Fatalf("err = %v, want the registry's own error to survive wrapping", err)
	}
}

// TestMergeManifest_SortsDigestsAndKeepsTagOrder covers the two orderings,
// which differ. Digest markers are read from a directory, so their order is
// whatever the filesystem gives and is sorted for a reproducible manifest;
// tags are a caller-declared list and keep the order they were written in.
//
// The fixture tells both apart: the markers are written b then a and expected
// a then b, while the tags are v1 then latest and expected in that order,
// which sorting would reverse. The old name said both were sorted.
func TestMergeManifest_SortsDigestsAndKeepsTagOrder(t *testing.T) {
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

	if !slices.Equal(reg.mergedDigests, []string{digestA, digestB}) {
		t.Errorf("digests = %v, want sorted [a… b…]", reg.mergedDigests)
	}

	if !slices.Equal(reg.mergedTags, []string{"ghcr.io/org/app:v1", "ghcr.io/org/app:latest"}) {
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
	if !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), "invalid digest marker") {
		t.Fatalf("err = %v, want ErrUsage naming the bad marker", err)
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

	reg := &fakeManifestRegistry{}

	err := appcontainer.MergeManifest(context.Background(), reg, io.Discard, appcontainer.MergeManifestInput{ImageName: "img", DigestsDir: dir})
	if !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), "tags is required") {
		t.Fatalf("err = %v, want ErrUsage naming the missing tags", err)
	}

	// A merge with no tags would publish an index nothing points at.
	if reg.mergeCalls != 0 {
		t.Error("merged despite having no tags to publish under")
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

	info, err := os.Stat(want)
	if err != nil {
		t.Fatalf("digest marker missing: %v", err)
	}

	if info.Size() != 0 || info.Mode().Perm() != 0o644 {
		t.Errorf("marker size=%d mode=%o, want empty mode 0644", info.Size(), info.Mode().Perm())
	}
}

func TestWriteDigestMarker_RejectsInvalidDigest(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "absent")

	_, err := appcontainer.WriteDigestMarker(appcontainer.WriteDigestMarkerInput{Digest: "sha256:nothex", DigestsDir: dir})
	if !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), "64 hex chars") {
		t.Fatalf("err = %v, want ErrUsage naming the digest shape", err)
	}

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("invalid digest created a directory: %v", err)
	}
}

// assertInspected checks InspectManifest fetched the manifest and resolved the
// digest for exactly the expected image reference.
func assertInspected(t *testing.T, reg *fakeManifestRegistry, image string) {
	t.Helper()

	if !slices.Equal(reg.manifestCalls, []string{image}) {
		t.Errorf("Manifest calls = %v, want [%s]", reg.manifestCalls, image)
	}

	if !slices.Equal(reg.resolveCalls, []string{image}) {
		t.Errorf("ResolveDigest calls = %v, want [%s]", reg.resolveCalls, image)
	}
}

// TestInspectManifest_TracesEachPhaseInOrder records every registry call,
// output and log byte for the three ways inspection ends. The explicit image
// wins over the tag list and is canonicalised before any request; the
// manifest is fetched, then the digest resolved, then the three outputs
// written in their documented order. A malformed digest stops after both
// calls with nothing written; a failed fetch stops before resolving and
// before logging.
func TestInspectManifest_TracesEachPhaseInOrder(t *testing.T) {
	t.Parallel()

	in := appcontainer.InspectManifestInput{Image: "docker://ghcr.io/org/app:v1", Tags: "ghcr.io/org/other:v2"}

	for name, tc := range map[string]struct {
		reg        *fakeManifestRegistry
		wantEvents []string
		wantKeys   []string
		wantLog    string
		wantErr    error
	}{
		"success": {
			reg:        &fakeManifestRegistry{digest: oneDigest, manifest: []byte(`{"schemaVersion":2}`)},
			wantEvents: []string{"manifest:ghcr.io/org/app:v1", "resolve:ghcr.io/org/app:v1"},
			wantKeys:   []string{"image", "digest", "image-digest-ref"},
			wantLog:    "{\"schemaVersion\":2}\n",
		},
		"malformed digest": {
			reg:        &fakeManifestRegistry{digest: "sha256:short", manifest: []byte(`{"schemaVersion":2}`)},
			wantEvents: []string{"manifest:ghcr.io/org/app:v1", "resolve:ghcr.io/org/app:v1"},
			wantLog:    "{\"schemaVersion\":2}\n",
			wantErr:    errs.ErrInvalidConfig,
		},
		"fetch failure": {
			reg:        &fakeManifestRegistry{digest: oneDigest, manifestErr: errRegistryUnreachable},
			wantEvents: []string{"manifest:ghcr.io/org/app:v1"},
			wantErr:    errRegistryUnreachable,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sink := fakeoutputsink.New(t)

			var log bytes.Buffer

			_, err := appcontainer.InspectManifest(context.Background(), tc.reg, sink, &log, in)
			if tc.wantErr == nil && err != nil || tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}

			if !slices.Equal(tc.reg.events, tc.wantEvents) {
				t.Errorf("registry calls = %q, want %q", tc.reg.events, tc.wantEvents)
			}

			if got := sink.Order(); !slices.Equal(got, tc.wantKeys) {
				t.Errorf("outputs = %q, want %q", got, tc.wantKeys)
			}

			if log.String() != tc.wantLog {
				t.Errorf("log = %q, want %q", log.String(), tc.wantLog)
			}
		})
	}
}

// TestMergeManifest_EndsAtEachDirectoryAndMergerBoundary covers where the
// digests come from and what the merger does with them. A missing or empty
// directory is missing input -- no platform build handed over a digest --
// and never reaches the registry; the empty case used to be reported as a
// configuration error. A merger failure is returned as itself after exactly
// one call, with no success line; success logs one exact line.
func TestMergeManifest_EndsAtEachDirectoryAndMergerBoundary(t *testing.T) {
	t.Parallel()

	errMergeRejected := errors.New("registry rejected the index") //nolint:err113 // a unique value to find in the chain.

	for name, tc := range map[string]struct {
		markers   []string
		noDir     bool
		mergeErr  error
		wantErr   error
		wantCalls int
		wantLog   string
	}{
		"missing directory": {noDir: true, wantErr: errs.ErrMissingInput},
		"empty directory":   {wantErr: errs.ErrMissingInput},
		"merger failure":    {markers: []string{strings.Repeat("a", 64)}, mergeErr: errMergeRejected, wantErr: errMergeRejected, wantCalls: 1},
		"success": {
			markers:   []string{strings.Repeat("b", 64), strings.Repeat("a", 64)},
			wantCalls: 1,
			wantLog:   "Created manifest list for ghcr.io/org/app — 2 platform(s) at 2 tag(s)\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fsys := testfs.NewReal(t)

			dir := fsys.Path("digests")
			if !tc.noDir {
				dir = fsys.MkdirAll("digests")
			}

			for _, marker := range tc.markers {
				fsys.WriteFile("digests/"+marker, nil)
			}

			reg := &fakeManifestRegistry{mergeErr: tc.mergeErr}

			var log bytes.Buffer

			err := appcontainer.MergeManifest(context.Background(), reg, &log, appcontainer.MergeManifestInput{
				ImageName: "ghcr.io/org/app", Tags: "ghcr.io/org/app:v1\nghcr.io/org/app:latest", DigestsDir: dir,
			})
			if tc.wantErr == nil && err != nil || tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}

			if tc.wantErr != nil && errors.Is(err, errs.ErrInvalidConfig) {
				t.Errorf("err = %v, must not be reported as a configuration error", err)
			}

			if reg.mergeCalls != tc.wantCalls || log.String() != tc.wantLog {
				t.Errorf("merge calls = %d, log = %q; want %d and %q", reg.mergeCalls, log.String(), tc.wantCalls, tc.wantLog)
			}
		})
	}
}
