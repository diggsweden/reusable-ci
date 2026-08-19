// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package buildah_test

import (
	"context"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/buildah"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

// TestBuildSignerImage_ArgvShape covers the signer-image build, which had
// no coverage. The provenance labels are the point: they are what ties a
// published image back to the commit it was built from.
func TestBuildSignerImage_ArgvShape(t *testing.T) {
	for _, tc := range []struct {
		name     string
		title    string
		wantHas  []string
		wantMiss []string
	}{
		{
			name:  "with a title",
			title: "reusable-ci signer",
			wantHas: []string{
				"--label org.opencontainers.image.title=reusable-ci signer",
			},
		},
		{
			// An empty title omits the label rather than publishing an
			// empty one — the same rule the metadata labels follow.
			name:     "no title omits the label",
			wantMiss: []string{"org.opencontainers.image.title"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name.
			m.Add("buildah", `printf 'ARGV %s\n' "$*" >&2`)

			a := &buildah.Adapter{Bin: m.Path("buildah")}

			var stderr strings.Builder

			err := a.BuildSignerImage(context.Background(), container.SignerImageBuildToolRequest{
				AuthFile: "/tmp/auth.json", Platform: "linux/amd64",
				SourceURL: "https://codeberg.org/org/repo", Revision: "abc123",
				LocalImage: "localhost/signer:amd64", Containerfile: "Containerfile",
				Context: ".", Title: tc.title,
			}, &stderr)
			if err != nil {
				t.Fatal(err)
			}

			log := stderr.String()

			for _, want := range []string{
				"--isolation chroot",
				"--authfile /tmp/auth.json",
				"--platform linux/amd64",
				// Provenance: these two labels are how a published image
				// is traced back to its source and commit.
				"--label org.opencontainers.image.source=https://codeberg.org/org/repo",
				"--label org.opencontainers.image.revision=abc123",
				"--tag localhost/signer:amd64",
			} {
				if !strings.Contains(log, want) {
					t.Errorf("argv missing %q:\n%s", want, log)
				}
			}

			// Recorded, not endorsed: this build uses --pull=missing while
			// BuildManifest uses --pull=always, so a base image already in
			// local storage is reused here. If that is ever aligned, this
			// assertion is the prompt to decide which way.
			if !strings.Contains(log, "--pull=missing") {
				t.Errorf("argv missing --pull=missing (BuildManifest uses --pull=always):\n%s", log)
			}

			for _, has := range tc.wantHas {
				if !strings.Contains(log, has) {
					t.Errorf("argv missing %q:\n%s", has, log)
				}
			}

			for _, miss := range tc.wantMiss {
				if strings.Contains(log, miss) {
					t.Errorf("argv unexpectedly carries %q:\n%s", miss, log)
				}
			}
		})
	}
}

// TestRemoveManifest_MissingIsANoOp covers the error discrimination its
// doc comment describes: a manifest that is not there is a successful
// no-op (matching `buildah manifest rm ... || true`), while a failure to
// run buildah at all is still an error.
//
// Collapsing the two would either fail a cleanup step for having nothing
// to clean, or hide a broken toolchain behind a cleanup that never ran.
func TestRemoveManifest_MissingIsANoOp(t *testing.T) {
	t.Run("non-zero exit is swallowed", func(t *testing.T) {
		m := mockbinary.New(t)
		m.Add("buildah", `printf 'manifest not known\n' >&2; exit 1`)

		a := &buildah.Adapter{Bin: m.Path("buildah")}
		if err := a.RemoveManifest(context.Background(), "localhost/app:list", &strings.Builder{}); err != nil {
			t.Errorf("a missing manifest must be a no-op, got %v", err)
		}
	})

	t.Run("an unrunnable binary is still an error", func(t *testing.T) {
		a := &buildah.Adapter{Bin: t.TempDir() + "/does-not-exist"}
		if err := a.RemoveManifest(context.Background(), "localhost/app:list", &strings.Builder{}); err == nil {
			t.Error("a binary that cannot run must be an error, not a no-op")
		}
	})
}

// TestSignerImageManifestArgv covers the remaining manifest helpers in
// one place. They are thin, but each argv encodes something a reader
// cannot infer from the call site.
func TestSignerImageManifestArgv(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(a *buildah.Adapter, w *strings.Builder) error
		want string
	}{
		{
			name: "create",
			call: func(a *buildah.Adapter, w *strings.Builder) error {
				return a.CreateManifest(context.Background(), "localhost/app:list", w)
			},
			want: "manifest create localhost/app:list",
		},
		{
			// --arch and --os describe the entry inside the list. Wrong
			// values do not fail the push; they make a client pull the
			// wrong architecture. The ref is digest-pinned by the caller
			// and carries the docker:// transport so buildah resolves it
			// from the registry rather than local storage.
			name: "add",
			call: func(a *buildah.Adapter, w *strings.Builder) error {
				return a.AddManifest(context.Background(), container.SignerImageManifestAddToolRequest{
					AuthFile: "/tmp/auth.json", Arch: "arm64",
					LocalManifest: "localhost/app:list",
					Ref:           "registry.example/app@sha256:abc",
				}, w)
			},
			want: "manifest add --authfile /tmp/auth.json --arch arm64 --os linux localhost/app:list docker://registry.example/app@sha256:abc",
		},
		{
			// --all pushes every entry; --rm drops the local list once it
			// is published, so a later run does not append to a stale one.
			name: "push",
			call: func(a *buildah.Adapter, w *strings.Builder) error {
				return a.PushManifest(context.Background(), "/tmp/auth.json", "localhost/app:list", "registry.example/app:v1", w)
			},
			want: "manifest push --all --quiet --rm --authfile /tmp/auth.json localhost/app:list docker://registry.example/app:v1",
		},
		{
			name: "push image",
			call: func(a *buildah.Adapter, w *strings.Builder) error {
				return a.PushImage(context.Background(), "/tmp/auth.json", "localhost/app:amd64", "registry.example/app:staging", w)
			},
			want: "push --quiet --authfile /tmp/auth.json localhost/app:amd64 docker://registry.example/app:staging",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := mockbinary.New(t)
			m.Add("buildah", `printf 'ARGV %s\n' "$*" >&2`)

			a := &buildah.Adapter{Bin: m.Path("buildah")}

			var stderr strings.Builder

			if err := tc.call(a, &stderr); err != nil {
				t.Fatal(err)
			}

			got := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(stderr.String()), "ARGV"))
			if got != tc.want {
				t.Errorf("argv = %q\nwant %q", got, tc.want)
			}
		})
	}
}

// TestRawManifest_UsesSkopeo covers the one helper that shells out to
// skopeo rather than buildah, and returns the document rather than a
// status. The raw manifest is what the digest of a pushed image is
// computed from, so reading it through the wrong tool or ref would give
// a digest for something else.
func TestRawManifest_UsesSkopeo(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("skopeo", `printf '{"schemaVersion":2}'; printf 'ARGV %s\n' "$*" >&2`)

	a := &buildah.Adapter{SkopeoBin: m.Path("skopeo")}

	var stderr strings.Builder

	raw, err := a.RawManifest(context.Background(), "/tmp/auth.json", "registry.example/app@sha256:abc", &stderr)
	if err != nil {
		t.Fatal(err)
	}

	if string(raw) != `{"schemaVersion":2}` {
		t.Errorf("raw = %q, want the document skopeo printed", raw)
	}

	want := "inspect --raw --authfile /tmp/auth.json docker://registry.example/app@sha256:abc"
	if got := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(stderr.String()), "ARGV")); got != want {
		t.Errorf("argv = %q\nwant %q", got, want)
	}
}
