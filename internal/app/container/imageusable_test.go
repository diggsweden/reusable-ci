// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type fakeUsableImageRegistry struct {
	digest string
	arch   string
	labels map[string]string
	err    error
	refs   []string
	archs  []string
}

func (f *fakeUsableImageRegistry) InspectImage(_ context.Context, ref, arch string) (string, string, map[string]string, error) {
	f.refs = append(f.refs, ref)
	f.archs = append(f.archs, arch)

	return f.digest, f.arch, f.labels, f.err
}

func TestUsableImageDigest_ReturnsDigestWhenArchAndLabelsMatch(t *testing.T) {
	t.Parallel()

	digest := "sha256:" + strings.Repeat("a", 64)
	registry := &fakeUsableImageRegistry{
		digest: digest,
		arch:   "amd64",
		labels: map[string]string{"org.example.content-id": "cid"},
	}
	sink := fakeoutputsink.New(t)

	got, err := appcontainer.UsableImageDigest(context.Background(), registry, sink, appcontainer.UsableImageDigestInput{
		Ref:            "registry.example/app:staging-amd64",
		Arch:           "amd64",
		RequiredLabels: []string{"org.example.content-id=cid"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.Digest != digest || got.Ref != "registry.example/app:staging-amd64@"+digest {
		t.Fatalf("output = %+v", got)
	}

	if sink.Single("digest") != digest || sink.Single("ref") != got.Ref {
		t.Fatalf("sink digest=%q ref=%q", sink.Single("digest"), sink.Single("ref"))
	}

	if registry.refs[0] != "registry.example/app:staging-amd64" || registry.archs[0] != "amd64" {
		t.Fatalf("registry call refs=%v archs=%v", registry.refs, registry.archs)
	}
}

func TestUsableImageDigest_RejectsArchitectureMismatch(t *testing.T) {
	t.Parallel()

	registry := &fakeUsableImageRegistry{
		digest: "sha256:" + strings.Repeat("b", 64),
		arch:   "arm64",
	}

	_, err := appcontainer.UsableImageDigest(context.Background(), registry, fakeoutputsink.New(t), appcontainer.UsableImageDigestInput{
		Ref:  "registry.example/app:staging-amd64",
		Arch: "amd64",
	})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "not reusable") {
		t.Fatalf("err = %v, want not reusable validation", err)
	}
}

func TestUsableImageDigest_RejectsRequiredLabelMismatch(t *testing.T) {
	t.Parallel()

	registry := &fakeUsableImageRegistry{
		digest: "sha256:" + strings.Repeat("c", 64),
		arch:   "amd64",
		labels: map[string]string{"org.example.content-id": "other"},
	}

	_, err := appcontainer.UsableImageDigest(context.Background(), registry, fakeoutputsink.New(t), appcontainer.UsableImageDigestInput{
		Ref:            "registry.example/app:staging-amd64",
		Arch:           "amd64",
		RequiredLabels: []string{"org.example.content-id=cid"},
	})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "not reusable") {
		t.Fatalf("err = %v, want not reusable validation", err)
	}
}

func TestUsableImageDigest_RejectsMalformedRequiredLabel(t *testing.T) {
	t.Parallel()

	_, err := appcontainer.UsableImageDigest(context.Background(), &fakeUsableImageRegistry{}, fakeoutputsink.New(t), appcontainer.UsableImageDigestInput{
		Ref:            "registry.example/app:staging-amd64",
		Arch:           "amd64",
		RequiredLabels: []string{"org.example.content-id"},
	})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}
}

func TestUsableImageDigest_RejectsInvalidRegistryDigest(t *testing.T) {
	t.Parallel()

	registry := &fakeUsableImageRegistry{digest: "sha256:bad", arch: "amd64"}

	_, err := appcontainer.UsableImageDigest(context.Background(), registry, fakeoutputsink.New(t), appcontainer.UsableImageDigestInput{
		Ref:  "registry.example/app:staging-amd64",
		Arch: "amd64",
	})
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}
