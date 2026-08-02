// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
)

type fakeRefManifestRegistry struct {
	ref string
	raw []byte
}

func (f *fakeRefManifestRegistry) Manifest(_ context.Context, ref string) ([]byte, error) {
	f.ref = ref

	return f.raw, nil
}

func TestContainerfileArgDefault_ReadsBeforeFirstFrom(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "Containerfile")
	if err := os.WriteFile(path, []byte("ARG BASE_IMAGE=alpine:3.20\nFROM ${BASE_IMAGE}\nARG BASE_IMAGE=too-late\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := appcontainer.ContainerfileArgDefault(appcontainer.ContainerfileArgDefaultInput{File: path, Name: "BASE_IMAGE"})
	if err != nil {
		t.Fatal(err)
	}

	if got != "alpine:3.20" {
		t.Fatalf("default = %q", got)
	}
}

func TestRefHelpers_CanonicalizeImageRefs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "tag_digest", in: "gcr.io/distroless/cc-debian13:nonroot@sha256:index", want: "gcr.io/distroless/cc-debian13@sha256:index"},
		{name: "host_port", in: "example.com:5000/ns/image:tag@sha256:index", want: "example.com:5000/ns/image@sha256:index"},
		{name: "docker_transport", in: "docker://alpine:3.20", want: "alpine:3.20"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := appcontainer.CanonicalRef(tt.in)
			if err != nil {
				t.Fatal(err)
			}

			if got != tt.want {
				t.Fatalf("canonical ref = %q, want %q", got, tt.want)
			}
		})
	}

	if _, err := appcontainer.CanonicalRef("oci://example.invalid/app:tag"); err == nil || !strings.Contains(err.Error(), "transport other than docker://") {
		t.Fatalf("unsupported transport error = %v", err)
	}
}

func TestImageNameForRef_StripsTagAndDigest(t *testing.T) {
	t.Parallel()

	got, err := appcontainer.ImageNameForRef("example.com:5000/ns/image:tag@sha256:index")
	if err != nil {
		t.Fatal(err)
	}

	if got != "example.com:5000/ns/image" {
		t.Fatalf("image name = %q", got)
	}
}

func TestPlatformRef_ResolvesIndexChildDigest(t *testing.T) {
	t.Parallel()

	registry := &fakeRefManifestRegistry{raw: []byte(fmt.Sprintf(`{
  "manifests": [
    {"digest": "sha256:%064d", "platform": {"os": "linux", "architecture": "amd64"}},
    {"digest": "sha256:%064d", "platform": {"os": "linux", "architecture": "arm64", "variant": "v8"}}
  ]
}`, 1, 2))}

	got, err := appcontainer.PlatformRef(context.Background(), registry, appcontainer.PlatformRefInput{
		Ref:      "example.invalid/ns/app:tag@sha256:index",
		Platform: "linux/arm64/v8",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := fmt.Sprintf("example.invalid/ns/app@sha256:%064d", 2)
	if got != want {
		t.Fatalf("platform ref = %q, want %q", got, want)
	}

	if registry.ref != "example.invalid/ns/app@sha256:index" {
		t.Fatalf("inspected ref = %q", registry.ref)
	}
}

func TestPlatformRef_ReturnsCanonicalSingleManifest(t *testing.T) {
	t.Parallel()

	registry := &fakeRefManifestRegistry{raw: []byte(`{"schemaVersion": 2}`)}

	got, err := appcontainer.PlatformRef(context.Background(), registry, appcontainer.PlatformRefInput{
		Ref:      "example.invalid/ns/app:tag@sha256:single",
		Platform: "linux/amd64",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got != "example.invalid/ns/app@sha256:single" {
		t.Fatalf("platform ref = %q", got)
	}
}
