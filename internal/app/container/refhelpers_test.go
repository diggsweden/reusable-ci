// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

type fakeRefManifestRegistry struct {
	ref   string
	raw   []byte
	calls int
	err   error
}

func (f *fakeRefManifestRegistry) Manifest(_ context.Context, ref string) ([]byte, error) {
	f.ref = ref
	f.calls++

	return f.raw, f.err
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

	digest := "sha256:" + strings.Repeat("a", 64)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "tag_digest", in: "gcr.io/distroless/cc-debian13:nonroot@" + digest, want: "gcr.io/distroless/cc-debian13@" + digest},
		{name: "host_port", in: "example.com:5000/ns/image:tag@" + digest, want: "example.com:5000/ns/image@" + digest},
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
}

// TestCanonicalRef_RefusesAnotherTransport is its own test rather than a
// tail on the canonicalisation table: it is the refusal, not a further
// canonical form, and burying it after a loop hid it from the name.
func TestCanonicalRef_RefusesAnotherTransport(t *testing.T) {
	t.Parallel()

	_, err := appcontainer.CanonicalRef("oci://example.invalid/app:tag")
	if !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), "transport other than docker://") {
		t.Fatalf("err = %v, want ErrUsage naming the transport", err)
	}
}

func TestImageNameForRef_StripsTagAndDigest(t *testing.T) {
	t.Parallel()

	got, err := appcontainer.ImageNameForRef("example.com:5000/ns/image:tag@sha256:" + strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}

	if got != "example.com:5000/ns/image" {
		t.Fatalf("image name = %q", got)
	}
}

func TestPlatformRef_ResolvesIndexChildDigest(t *testing.T) {
	t.Parallel()

	indexDigest := "sha256:" + strings.Repeat("a", 64)

	registry := &fakeRefManifestRegistry{raw: []byte(fmt.Sprintf(`{
  "manifests": [
    {"digest": "sha256:%064d", "platform": {"os": "linux", "architecture": "amd64"}},
    {"digest": "sha256:%064d", "platform": {"os": "linux", "architecture": "arm64", "variant": "v8"}}
  ]
}`, 1, 2))}

	got, err := appcontainer.PlatformRef(context.Background(), registry, appcontainer.PlatformRefInput{
		Ref:      "example.invalid/ns/app:tag@" + indexDigest,
		Platform: "linux/arm64/v8",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := fmt.Sprintf("example.invalid/ns/app@sha256:%064d", 2)
	if got != want {
		t.Fatalf("platform ref = %q, want %q", got, want)
	}

	if registry.ref != "example.invalid/ns/app@"+indexDigest {
		t.Fatalf("inspected ref = %q", registry.ref)
	}
}

// TestPlatformRef_MatchesTheVariant covers the variant, which the index-child
// test cannot: its two children differ by architecture, so ignoring the
// variant entirely still picks the right one. Here both children are arm64 and
// only the variant tells them apart -- a wrong pick ships an image for a CPU
// the request did not ask for.
func TestPlatformRef_MatchesTheVariant(t *testing.T) {
	t.Parallel()

	indexDigest := "sha256:" + strings.Repeat("a", 64)

	registry := &fakeRefManifestRegistry{raw: []byte(fmt.Sprintf(`{
  "manifests": [
    {"digest": "sha256:%064d", "platform": {"os": "linux", "architecture": "arm64", "variant": "v7"}},
    {"digest": "sha256:%064d", "platform": {"os": "linux", "architecture": "arm64", "variant": "v8"}}
  ]
}`, 7, 8))}

	got, err := appcontainer.PlatformRef(context.Background(), registry, appcontainer.PlatformRefInput{
		Ref:      "example.invalid/ns/app:tag@" + indexDigest,
		Platform: "linux/arm64/v8",
	})
	if err != nil {
		t.Fatal(err)
	}

	if want := fmt.Sprintf("example.invalid/ns/app@sha256:%064d", 8); got != want {
		t.Errorf("platform ref = %q, want the v8 child %q", got, want)
	}
}

func TestPlatformRef_ReturnsCanonicalSingleManifest(t *testing.T) {
	t.Parallel()

	digest := "sha256:" + strings.Repeat("a", 64)

	registry := &fakeRefManifestRegistry{raw: []byte(`{"schemaVersion": 2}`)}

	got, err := appcontainer.PlatformRef(context.Background(), registry, appcontainer.PlatformRefInput{
		Ref:      "example.invalid/ns/app:tag@" + digest,
		Platform: "linux/amd64",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got != "example.invalid/ns/app@"+digest {
		t.Fatalf("platform ref = %q", got)
	}
}

// TestPlatformRef_QueriesTheRegistryWithTheCanonicalRef pins what is sent
// outward, which the success tests above do not.
//
// They assert the ref that comes back. On the single-manifest branch that
// happens to be the same string as the one queried, so it looks like
// coverage; on the index branch the return is rebuilt from the child digest
// and says nothing about the query. The query is the part that leaves the
// process: it is handed to a registry client, so a ref still carrying a
// "docker://" transport or a tag alongside a digest is a request no registry
// answers, and the failure surfaces as a transport error rather than as the
// normalisation bug it is.
func TestPlatformRef_QueriesTheRegistryWithTheCanonicalRef(t *testing.T) {
	t.Parallel()

	digest := "sha256:" + strings.Repeat("a", 64)

	for name, tc := range map[string]struct{ in, want string }{
		"a plain tag is already canonical": {
			in:   "example.invalid/ns/app:v1",
			want: "example.invalid/ns/app:v1",
		},
		"a docker:// transport is stripped": {
			in:   "docker://example.invalid/ns/app:v1",
			want: "example.invalid/ns/app:v1",
		},
		"a tag beside a digest loses the tag": {
			in:   "example.invalid/ns/app:v1@" + digest,
			want: "example.invalid/ns/app@" + digest,
		},
		"a transport and a tagged digest lose both": {
			in:   "docker://example.invalid/ns/app:v1@" + digest,
			want: "example.invalid/ns/app@" + digest,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			registry := &fakeRefManifestRegistry{raw: []byte(`{"schemaVersion":2}`)}

			if _, err := appcontainer.PlatformRef(context.Background(), registry, appcontainer.PlatformRefInput{
				Ref:      tc.in,
				Platform: "linux/amd64",
			}); err != nil {
				t.Fatal(err)
			}

			if registry.ref != tc.want {
				t.Errorf("the registry was queried for %q, want %q", registry.ref, tc.want)
			}

			if registry.calls != 1 {
				t.Errorf("the registry was queried %d time(s), want exactly 1", registry.calls)
			}
		})
	}
}

func TestPlatformRef_RejectsMalformedMatchingDigests(t *testing.T) {
	t.Parallel()

	for _, digest := range []string{"", "sha256:", "sha256:abc", "sha512:" + strings.Repeat("a", 128), "sha256:" + strings.Repeat("g", 64), "sha256:" + strings.Repeat("A", 64), "sha256:" + strings.Repeat("a", 65), "sha256:" + strings.Repeat("a", 64) + "\n"} {
		t.Run(digest, func(t *testing.T) {
			t.Parallel()

			encoded, err := json.Marshal(digest)
			if err != nil {
				t.Fatal(err)
			}

			registry := &fakeRefManifestRegistry{raw: []byte(`{"manifests":[{"digest":` + string(encoded) + `,"platform":{"os":"linux","architecture":"arm64","variant":"v8"}}]}`)}

			got, err := appcontainer.PlatformRef(t.Context(), registry, appcontainer.PlatformRefInput{Ref: "example.invalid/app:tag", Platform: "linux/arm64/v8"})
			if !errors.Is(err, errs.ErrMalformedInput) || got != "" {
				t.Errorf("ref=%q err=%v, want empty and ErrMalformedInput", got, err)
			}
		})
	}
}

// TestPlatformRef_RefusesBadInputWithoutTouchingTheRegistry covers each way the
// inputs can be wrong, and requires the registry to stay untouched.
//
// The success tests read the recorded ref, so they show the registry IS called
// on the happy path; nothing showed it is not called when the input is refused.
// Order matters here for a plain reason: the registry call is a network round
// trip against a reference this function has not yet validated, so validating
// afterwards means every malformed platform string and every unparsable ref
// still reaches out first.
func TestPlatformRef_RefusesBadInputWithoutTouchingTheRegistry(t *testing.T) {
	t.Parallel()

	const goodRef = "ghcr.io/org/app:v1"

	for _, tc := range []struct {
		name string
		in   appcontainer.PlatformRefInput
		want error
	}{
		{
			name: "an empty platform",
			in:   appcontainer.PlatformRefInput{Ref: goodRef},
			want: errs.ErrUsage,
		},
		{
			name: "a platform with no architecture",
			in:   appcontainer.PlatformRefInput{Platform: "linux", Ref: goodRef},
			want: errs.ErrUsage,
		},
		{
			name: "a platform with too many components",
			in:   appcontainer.PlatformRefInput{Platform: "linux/amd64/v2/extra", Ref: goodRef},
			want: errs.ErrUsage,
		},
		{
			name: "an empty ref",
			in:   appcontainer.PlatformRefInput{Platform: "linux/amd64"},
			want: errs.ErrUsage,
		},
		{
			name: "a ref carrying a foreign transport",
			in:   appcontainer.PlatformRefInput{Platform: "linux/amd64", Ref: "oci://ghcr.io/org/app:v1"},
			want: errs.ErrUsage,
		},
		{
			name: "a ref with two digest separators",
			in:   appcontainer.PlatformRefInput{Platform: "linux/amd64", Ref: goodRef + "@sha256:a@sha256:b"},
			want: errs.ErrUsage,
		},
		{
			name: "a ref with an embedded newline",
			in:   appcontainer.PlatformRefInput{Platform: "linux/amd64", Ref: "ghcr.io/org/app:v1\nrm -rf /"},
			want: errs.ErrUsage,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			registry := &fakeRefManifestRegistry{raw: []byte(`{"schemaVersion":2}`)}

			got, err := appcontainer.PlatformRef(context.Background(), registry, tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}

			if got != "" {
				t.Errorf("a refused input returned the ref %q", got)
			}

			if registry.calls != 0 {
				t.Errorf("the registry was queried %d time(s) for an input that was refused", registry.calls)
			}
		})
	}

	// A nil registry is the caller's wiring mistake, and it must be named as
	// one rather than panicking.
	if _, err := appcontainer.PlatformRef(context.Background(), nil,
		appcontainer.PlatformRefInput{Platform: "linux/amd64", Ref: goodRef},
	); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("nil registry: err = %v, want ErrUsage", err)
	}
}

// TestPlatformRef_SelectsOnlyTheRequestedPlatform covers the selection
// branches the tests above leave open. Their wrong children differ from the
// right one by architecture alone or by variant alone, so ignoring the OS, or
// ignoring the architecture when the variant matches, still picks correctly.
// Here the wrong child comes first and shares everything but one field.
//
// The three failures are kept apart: a document that is not JSON is
// malformed, an index without the platform is a validation failure, and two
// matching children are not an error at all -- the OCI image-index
// specification says the first matching entry SHOULD be used.
func TestPlatformRef_SelectsOnlyTheRequestedPlatform(t *testing.T) {
	t.Parallel()

	child := func(n int) string { return fmt.Sprintf("sha256:%064d", n) }

	for name, tc := range map[string]struct {
		raw     string
		want    string
		wantErr error
	}{
		"wrong architecture with the same variant": {
			raw:  `{"manifests":[{"digest":"` + child(1) + `","platform":{"os":"linux","architecture":"arm","variant":"v8"}},{"digest":"` + child(2) + `","platform":{"os":"linux","architecture":"arm64","variant":"v8"}}]}`,
			want: "example.invalid/ns/app@" + child(2),
		},
		"wrong os with the same architecture and variant": {
			raw:  `{"manifests":[{"digest":"` + child(1) + `","platform":{"os":"freebsd","architecture":"arm64","variant":"v8"}},{"digest":"` + child(2) + `","platform":{"os":"linux","architecture":"arm64","variant":"v8"}}]}`,
			want: "example.invalid/ns/app@" + child(2),
		},
		"two matching children use the first": {
			raw:  `{"manifests":[{"digest":"` + child(3) + `","platform":{"os":"linux","architecture":"arm64","variant":"v8"}},{"digest":"` + child(4) + `","platform":{"os":"linux","architecture":"arm64","variant":"v8"}}]}`,
			want: "example.invalid/ns/app@" + child(3),
		},
		"a document that is not JSON": {
			raw:     `{"manifests":[`,
			wantErr: errs.ErrMalformedInput,
		},
		"an index without the platform": {
			raw:     `{"manifests":[{"digest":"` + child(1) + `","platform":{"os":"linux","architecture":"amd64"}}]}`,
			wantErr: errs.ErrValidation,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			registry := &fakeRefManifestRegistry{raw: []byte(tc.raw)}

			got, err := appcontainer.PlatformRef(t.Context(), registry, appcontainer.PlatformRefInput{
				Ref:      "example.invalid/ns/app:tag",
				Platform: "linux/arm64/v8",
			})

			if tc.wantErr == nil {
				if err != nil || got != tc.want {
					t.Errorf("ref = %q, err = %v; want %q", got, err, tc.want)
				}

				return
			}

			// The two failure sentinels must not both hold, or a caller cannot
			// tell a broken registry response from an image built without the
			// platform.
			other := errs.ErrValidation
			if errors.Is(tc.wantErr, errs.ErrValidation) {
				other = errs.ErrMalformedInput
			}

			if !errors.Is(err, tc.wantErr) || errors.Is(err, other) || got != "" {
				t.Errorf("ref = %q, err = %v; want empty and only %v", got, err, tc.wantErr)
			}
		})
	}
}
