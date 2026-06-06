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

type fakeDockerOps struct {
	digestOut  string
	digestErr  string
	runErr     error
	inheritErr error
	calls      [][]string
}

func (f *fakeDockerOps) Run(_ context.Context, args ...string) (string, string, error) {
	f.calls = append(f.calls, append([]string(nil), args...))

	return f.digestOut, f.digestErr, f.runErr
}

func (f *fakeDockerOps) RunInherit(_ context.Context, out, _ io.Writer, args ...string) error {
	f.calls = append(f.calls, append([]string(nil), args...))

	if out != nil {
		_, _ = io.WriteString(out, "inspect output\n")
	}

	return f.inheritErr
}

func TestInspectManifest_UsesExplicitImage(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	docker := &fakeDockerOps{digestOut: "sha256:1111111111111111111111111111111111111111111111111111111111111111\n"}

	var out bytes.Buffer

	res, err := appcontainer.InspectManifest(context.Background(), docker, sink, &out, &bytes.Buffer{}, appcontainer.InspectManifestInput{Image: "ghcr.io/org/app:v1"}) //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	if err != nil {
		t.Fatal(err)
	}

	if res.Image != "ghcr.io/org/app:v1" || res.Digest != "sha256:1111111111111111111111111111111111111111111111111111111111111111" {
		t.Fatalf("res = %+v", res)
	}

	if got := sink.Single("image"); got != "ghcr.io/org/app:v1" {
		t.Errorf("image output = %q", got)
	}

	if got := sink.Single("digest"); got != "sha256:1111111111111111111111111111111111111111111111111111111111111111" {
		t.Errorf("digest output = %q", got)
	}

	// image-digest-ref must be the cosign-ready immutable form: the
	// bare image (tag stripped) joined to the digest with `@`.
	if got := sink.Single("image-digest-ref"); got != "ghcr.io/org/app@sha256:1111111111111111111111111111111111111111111111111111111111111111" {
		t.Errorf("image-digest-ref output = %q, want ghcr.io/org/app@sha256:1111111111111111111111111111111111111111111111111111111111111111", got)
	}

	if !strings.Contains(out.String(), "inspect output") {
		t.Errorf("out = %q", out.String())
	}

	assertInspectCalls(t, docker.calls, "ghcr.io/org/app:v1")
}

func TestInspectManifest_RejectsMalformedDigest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		digestOut string
	}{
		{"missing sha256 prefix", "abc1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcd"},
		{"wrong algorithm", "sha512:abc1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcd"},
		{"too short", "sha256:abc123"},
		{"uppercase hex", "sha256:ABC1234567890ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890ABCD"},
		{"trailing garbage", "sha256:abc1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcd extra"},
		{"json wrapper", `{"digest":"sha256:1111111111111111111111111111111111111111111111111111111111111111..."}`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			sink := fakeoutputsink.New(t)
			docker := &fakeDockerOps{digestOut: c.digestOut + "\n"}

			_, err := appcontainer.InspectManifest(context.Background(), docker, sink, io.Discard, io.Discard, appcontainer.InspectManifestInput{Image: "ghcr.io/org/app:v1"})
			if err == nil {
				t.Fatalf("expected rejection for malformed digest %q, got nil error", c.digestOut)
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
		// stripTag is package-private; we exercise it via the
		// public InspectManifest output to avoid widening the API.
		sink := fakeoutputsink.New(t)
		docker := &fakeDockerOps{digestOut: "sha256:1111111111111111111111111111111111111111111111111111111111111111"}

		_, err := appcontainer.InspectManifest(context.Background(), docker, sink, io.Discard, io.Discard, appcontainer.InspectManifestInput{Image: c.in})
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}

		if got := sink.Single("image-digest-ref"); got != c.want+"@sha256:1111111111111111111111111111111111111111111111111111111111111111" {
			t.Errorf("stripTag(%q) → image-digest-ref = %q, want %s@sha256:1111111111111111111111111111111111111111111111111111111111111111", c.in, got, c.want)
		}
	}
}

func TestInspectManifest_UsesFirstTagWhenImageEmpty(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	docker := &fakeDockerOps{digestOut: "sha256:2222222222222222222222222222222222222222222222222222222222222222"}

	_, err := appcontainer.InspectManifest(context.Background(), docker, sink, io.Discard, io.Discard, appcontainer.InspectManifestInput{Tags: "\n  ghcr.io/org/app:v1\n  ghcr.io/org/app:latest\n"})
	if err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("image"); got != "ghcr.io/org/app:v1" {
		t.Errorf("image output = %q", got)
	}

	assertInspectCalls(t, docker.calls, "ghcr.io/org/app:v1")
}

func TestInspectManifest_RequiresImageOrTags(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	_, err := appcontainer.InspectManifest(context.Background(), &fakeDockerOps{}, sink, io.Discard, io.Discard, appcontainer.InspectManifestInput{})
	if err == nil || !strings.Contains(err.Error(), "image or tags is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestInspectManifest_PropagatesInspectFailure(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	mockErr := errors.New("inspect failed") //nolint:err113 // test mock error.

	_, err := appcontainer.InspectManifest(context.Background(), &fakeDockerOps{inheritErr: mockErr}, sink, io.Discard, io.Discard, appcontainer.InspectManifestInput{Image: "img"}) //nolint:goconst // test fixture image name.
	if err == nil || !strings.Contains(err.Error(), "inspect failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestInspectManifest_RejectsEmptyDigest(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	_, err := appcontainer.InspectManifest(context.Background(), &fakeDockerOps{digestOut: "\n"}, sink, io.Discard, io.Discard, appcontainer.InspectManifestInput{Image: "img"})
	if err == nil || !strings.Contains(err.Error(), "empty manifest digest") {
		t.Fatalf("err = %v", err)
	}
}

func TestMergeManifest_CreatesTagsFromDigestFiles(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.MkdirAll("digests")
	a := strings.Repeat("a", 64) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	b := strings.Repeat("b", 64) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	fsys.WriteFile("digests/"+b, nil)
	fsys.WriteFile("digests/"+a, nil)

	docker := &fakeDockerOps{}

	if err := appcontainer.MergeManifest(context.Background(), docker, io.Discard, io.Discard, appcontainer.MergeManifestInput{
		ImageName:  "ghcr.io/org/app",
		Tags:       "ghcr.io/org/app:v1\nghcr.io/org/app:latest\n",
		DigestsDir: dir,
	}); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"buildx", "imagetools", "create", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"-t", "ghcr.io/org/app:v1",
		"-t", "ghcr.io/org/app:latest",
		"ghcr.io/org/app@sha256:" + a,
		"ghcr.io/org/app@sha256:" + b,
	}
	if !reflect.DeepEqual(docker.calls[0], want) {
		t.Fatalf("call = %#v, want %#v", docker.calls[0], want)
	}
}

func TestMergeManifest_RejectsInvalidDigestMarker(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.MkdirAll("digests")
	fsys.WriteFile("digests/not-a-digest", nil)

	err := appcontainer.MergeManifest(context.Background(), &fakeDockerOps{}, io.Discard, io.Discard, appcontainer.MergeManifestInput{
		ImageName:  "ghcr.io/org/app",
		Tags:       "ghcr.io/org/app:v1",
		DigestsDir: dir,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid digest marker") {
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

func TestMergeManifest_RequiresTags(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.MkdirAll("digests")
	fsys.WriteFile("digests/"+strings.Repeat("a", 64), nil)

	err := appcontainer.MergeManifest(context.Background(), &fakeDockerOps{}, io.Discard, io.Discard, appcontainer.MergeManifestInput{ImageName: "img", DigestsDir: dir})
	if err == nil || !strings.Contains(err.Error(), "tags is required") {
		t.Fatalf("err = %v", err)
	}
}

func assertInspectCalls(t *testing.T, got [][]string, image string) {
	t.Helper()

	want := [][]string{
		{"buildx", "imagetools", "inspect", image},
		{"buildx", "imagetools", "inspect", image, "--format", "{{.Manifest.Digest}}"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %#v, want %#v", got, want)
	}
}
