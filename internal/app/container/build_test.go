// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"context"
	"io"
	"reflect"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

// fakeBuilder records the request it was handed for either build path.
type fakeBuilder struct {
	got         container.BuildRequest
	builtLayout bool
}

func (f *fakeBuilder) Build(_ context.Context, req container.BuildRequest, _ io.Writer) error {
	f.got = req

	return nil
}

func (f *fakeBuilder) BuildToLayout(_ context.Context, req container.BuildRequest, _ string, _ io.Writer) error {
	f.got = req
	f.builtLayout = true

	return nil
}

// fakePusher records the ref and returns a canned digest.
type fakePusher struct {
	gotRef string
	digest string
}

func (f *fakePusher) PushLayoutByDigest(_ context.Context, _, imageRef string) (string, error) {
	f.gotRef = imageRef

	return f.digest, nil
}

// TestBuildImage_PushByDigestHandsTheBuilderEveryInput covers push-by-digest:
// the build goes through a layout, the pushed digest is what comes back and
// what is published as an output, and the request the builder receives carries
// every input it was given.
//
// The request is compared whole. It was checked five fields at a time with a
// condition that reported the whole struct, and the fields it did not name --
// build arguments and labels among them -- could have been dropped without
// failing anything, despite being the flags in the old name.
func TestBuildImage_PushByDigestHandsTheBuilderEveryInput(t *testing.T) {
	t.Parallel()

	builder := &fakeBuilder{}
	pusher := &fakePusher{digest: "sha256:abc123"}
	sink := fakeoutputsink.New(t)

	in := appcontainer.BuildImageInput{
		Context:         ".",
		Containerfile:   "Containerfile",
		Target:          "build",
		Platform:        "linux/arm64",
		BuildArgs:       []string{"VERSION=1.2.3"},
		Secrets:         []string{"id=db,src=/run/secrets/db"},
		Labels:          []string{"org.opencontainers.image.version=1.2.3"},
		SourceDateEpoch: "1700000000",
		CacheRepo:       "ghcr.io/org/buildcache",
		CacheScope:      "app-arm64",
		CachePush:       true,
		Mode:            container.BuildModePushByDigest,
		ImageRef:        "ghcr.io/org/repo",
	}

	got, err := appcontainer.BuildImage(context.Background(), builder, pusher, sink, io.Discard, in)
	if err != nil {
		t.Fatal(err)
	}

	if got != "sha256:abc123" {
		t.Errorf("returned digest = %q, want the digest the push reported", got)
	}

	if v := sink.Single("digest"); v != "sha256:abc123" {
		t.Errorf("sink digest = %q, want sha256:abc123", v)
	}

	// Push-by-digest builds to an OCI layout and pushes that, rather than
	// building straight to a tag.
	if !builder.builtLayout {
		t.Error("push-by-digest must go through BuildToLayout")
	}

	if pusher.gotRef != "ghcr.io/org/repo" {
		t.Errorf("pushed to %q, want ghcr.io/org/repo", pusher.gotRef)
	}

	wantRequest := container.BuildRequest{
		Context:         ".",
		Containerfile:   "Containerfile",
		Target:          "build",
		Platform:        "linux/arm64",
		BuildArgs:       []string{"VERSION=1.2.3"},
		Secrets:         []string{"id=db,src=/run/secrets/db"},
		Labels:          []string{"org.opencontainers.image.version=1.2.3"},
		SourceDateEpoch: "1700000000",
		CacheRepo:       "ghcr.io/org/buildcache",
		CacheScope:      "app-arm64",
		CachePush:       true,
		Mode:            container.BuildModePushByDigest,
		ImageRef:        "ghcr.io/org/repo",
	}
	if !reflect.DeepEqual(builder.got, wantRequest) {
		t.Errorf("build request =\n%+v\nwant\n%+v", builder.got, wantRequest)
	}

	// The cache reference is assembled by the domain rather than re-encoded
	// by callers, so it is worth naming separately from the two fields.
	if ref := builder.got.CacheRef(); ref != "ghcr.io/org/buildcache/app-arm64" {
		t.Errorf("cache ref = %q, want ghcr.io/org/buildcache/app-arm64", ref)
	}
}

func TestBuildImage_LoadMode_EmitsNoDigest(t *testing.T) {
	t.Parallel()

	builder := &fakeBuilder{}
	sink := fakeoutputsink.New(t)

	if _, err := appcontainer.BuildImage(context.Background(), builder, &fakePusher{}, sink, io.Discard, appcontainer.BuildImageInput{
		Context:  ".",
		Mode:     container.BuildModeLoad,
		ImageRef: "local:verify",
	}); err != nil {
		t.Fatal(err)
	}

	if builder.builtLayout {
		t.Error("load mode must not go through BuildToLayout")
	}

	if v := sink.Single("digest"); v != "" {
		t.Errorf("load mode wrote a digest output %q; want none", v)
	}
}

func TestBuildImage_RejectsInvalidRequestBeforeBuilding(t *testing.T) {
	t.Parallel()

	builder := &fakeBuilder{}

	// load mode without an image ref is a usage error caught before the builder runs.
	if _, err := appcontainer.BuildImage(context.Background(), builder, &fakePusher{}, fakeoutputsink.New(t), io.Discard,
		appcontainer.BuildImageInput{Context: ".", Mode: container.BuildModeLoad}); err == nil {
		t.Fatal("expected validation error for load mode without image-ref")
	}

	if builder.got.Context != "" {
		t.Error("builder was invoked despite an invalid request")
	}
}
