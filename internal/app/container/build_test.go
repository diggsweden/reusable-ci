// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"context"
	"io"
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

func TestBuildImage_PushByDigest_EmitsDigestAndThreadsFlags(t *testing.T) {
	t.Parallel()

	builder := &fakeBuilder{}
	pusher := &fakePusher{digest: "sha256:abc123"}
	sink := fakeoutputsink.New(t)

	got, err := appcontainer.BuildImage(context.Background(), builder, pusher, sink, io.Discard, appcontainer.BuildImageInput{
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
	})
	if err != nil {
		t.Fatal(err)
	}

	if got != "sha256:abc123" {
		t.Errorf("returned digest = %q", got)
	}

	if !builder.builtLayout {
		t.Error("push-by-digest must go through BuildToLayout")
	}

	if v := sink.Single("digest"); v != "sha256:abc123" {
		t.Errorf("sink digest = %q, want sha256:abc123", v)
	}

	if pusher.gotRef != "ghcr.io/org/repo" {
		t.Errorf("pusher got ref %q", pusher.gotRef)
	}

	r := builder.got
	if r.Target != "build" || r.Platform != "linux/arm64" || r.SourceDateEpoch != "1700000000" ||
		r.CacheRef() != "ghcr.io/org/buildcache:app-arm64" || !r.CachePush {
		t.Errorf("request not threaded: %+v", r)
	}

	if len(r.Secrets) != 1 || r.Secrets[0] != "id=db,src=/run/secrets/db" {
		t.Errorf("secrets not threaded: %v", r.Secrets)
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
