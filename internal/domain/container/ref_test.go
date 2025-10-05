// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestValidDigest_AcceptsOnlySHA256HexOfTheRightLength(t *testing.T) {
	t.Parallel()

	valid := "sha256:" + strings.Repeat("a", 64)
	for _, ok := range []string{valid, "sha256:" + strings.Repeat("0", 64)} {
		if !container.ValidDigest(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}

	for _, bad := range []string{
		"",
		strings.Repeat("a", 64),             // missing sha256: prefix
		"sha256:" + strings.Repeat("a", 63), // too short
		"sha256:" + strings.Repeat("A", 64), // uppercase not allowed
		"sha512:" + strings.Repeat("a", 64), // wrong algo
	} {
		if container.ValidDigest(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestValidDigestPinnedRef_UsesStrictOCIParsingAndRefusesTags(t *testing.T) {
	t.Parallel()

	digest := "sha256:" + strings.Repeat("a", 64)
	for _, ref := range []string{
		"ghcr.io/owner/image@" + digest,
		"[::1]:5000/owner/image@" + digest,
	} {
		if !container.ValidDigestPinnedRef(ref) {
			t.Errorf("%q should be a valid digest-pinned ref", ref)
		}
	}

	for _, ref := range []string{
		"ghcr.io/owner/image:tag@" + digest,
		"ghcr.io/owner/../image@" + digest,
		"ghcr.io/owner/image@sha256:short",
		"owner/image@" + digest,
	} {
		if container.ValidDigestPinnedRef(ref) {
			t.Errorf("%q should not be a valid digest-pinned ref", ref)
		}
	}
}

func TestValidDigestReference_AcceptsOptionalSourceTag(t *testing.T) {
	t.Parallel()

	digest := "sha256:" + strings.Repeat("a", 64)
	for _, ref := range []string{
		"ghcr.io/owner/image@" + digest,
		"ghcr.io/owner/image:staging@" + digest,
		"[::1]:5000/owner/image:staging@" + digest,
	} {
		if !container.ValidDigestReference(ref) {
			t.Errorf("%q should be a valid digest reference", ref)
		}
	}

	for _, ref := range []string{
		"owner/image@" + digest,
		"ghcr.io/owner/Image@" + digest,
		"ghcr.io/owner/image:bad tag@" + digest,
		"ghcr.io/owner/image@sha256:short",
	} {
		if container.ValidDigestReference(ref) {
			t.Errorf("%q should not be a valid digest reference", ref)
		}
	}
}

func TestValidTaggedRef_UsesStrictFullyQualifiedOCIParsing(t *testing.T) {
	t.Parallel()

	for _, ref := range []string{
		"ghcr.io/owner/image:latest",
		"[::1]:5000/owner/image:v1.2.3",
	} {
		if !container.ValidTaggedRef(ref) {
			t.Errorf("%q should be a valid tagged ref", ref)
		}
	}

	for _, ref := range []string{
		"owner/image:latest",
		"ghcr.io/owner/image",
		"ghcr.io/owner/image:bad tag",
		"ghcr.io/owner/BadName:latest",
		"ghcr.io/owner/../image:latest",
	} {
		if container.ValidTaggedRef(ref) {
			t.Errorf("%q should not be a valid tagged ref", ref)
		}
	}
}

func TestCanonicalImageRef_RejectsMalformedOCIReferences(t *testing.T) {
	t.Parallel()

	for _, ref := range []string{
		"ghcr.io/owner/../image:tag",
		"ghcr.io/owner/image@sha256:short",
		"ghcr.io/owner/image:bad tag",
	} {
		if _, err := container.CanonicalImageRef(ref); !errors.Is(err, errs.ErrUsage) {
			t.Errorf("CanonicalImageRef(%q) err = %v, want ErrUsage", ref, err)
		}
	}
}

func TestStripTag_RemovesTheTagAndKeepsAHostPort(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"ghcr.io/org/app:v1.2.3": "ghcr.io/org/app",    // registry + tag
		"ghcr.io/org/app":        "ghcr.io/org/app",    // already bare (idempotent)
		"localhost:5000/img:tag": "localhost:5000/img", // host:port preserved
		"localhost:5000/img":     "localhost:5000/img", // host:port, no tag
		"alpine:3.21":            "alpine",             // registry-less
	}
	for in, want := range cases {
		if got := container.StripTag(in); got != want {
			t.Errorf("StripTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripTagOrDigest_RemovesTagDigestOrBoth(t *testing.T) {
	t.Parallel()

	digest := "sha256:" + strings.Repeat("a", 64)

	cases := map[string]string{
		"ghcr.io/org/app:v1.2.3":        "ghcr.io/org/app",
		"ghcr.io/org/app@" + digest:     "ghcr.io/org/app",
		"ghcr.io/org/app:tag@" + digest: "ghcr.io/org/app",
		"localhost:5000/img@" + digest:  "localhost:5000/img",
		"localhost:5000/img:tag":        "localhost:5000/img",
	}
	for in, want := range cases {
		if got := container.StripTagOrDigest(in); got != want {
			t.Errorf("StripTagOrDigest(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRegistryHost_ParsesURLAndOCIForms(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]string{
		"https://registry.example.test/group/project": "registry.example.test",
		"registry.example.test/group/project":         "registry.example.test",
		"localhost:5000/group/project":                "localhost:5000",
		"ghcr.io":                                     "ghcr.io",
	} {
		got, err := container.RegistryHost(raw)
		if err != nil {
			t.Fatalf("RegistryHost(%q): %v", raw, err)
		}

		if got != want {
			t.Errorf("RegistryHost(%q) = %q, want %q", raw, got, want)
		}
	}

	for _, raw := range []string{"", "https://", "registry.example.test/bad path"} {
		if _, err := container.RegistryHost(raw); !errors.Is(err, errs.ErrUsage) {
			t.Errorf("RegistryHost(%q) err = %v, want ErrUsage", raw, err)
		}
	}
}

// TestValidDigest_BoundariesAndAlphabet covers the two ends the existing table
// leaves open.
//
// It checks 63 characters and uppercase, so the length floor and the case rule
// are pinned. It does not check 64+1, and it does not check a lowercase
// character outside the hexadecimal alphabet — so a pattern that lost its
// trailing anchor, or widened [0-9a-f] to something like [0-9a-z], passes every
// case. Both are the difference between "this is a sha256 digest" and "this
// looks a bit like one", and the whole point of a digest pin is that it is
// exact.
func TestValidDigest_BoundariesAndAlphabet(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "exactly 64 hex", value: "sha256:" + strings.Repeat("a", 64), want: true},
		{name: "all digits", value: "sha256:" + strings.Repeat("9", 64), want: true},
		{name: "full alphabet", value: "sha256:" + strings.Repeat("0123456789abcdef", 4), want: true},
		{name: "65 characters", value: "sha256:" + strings.Repeat("a", 65)},
		{name: "63 characters", value: "sha256:" + strings.Repeat("a", 63)},
		{name: "lowercase non-hex", value: "sha256:" + strings.Repeat("g", 64)},
		{name: "one non-hex character among hex", value: "sha256:" + strings.Repeat("a", 63) + "z"},
		{name: "trailing text after a valid digest", value: "sha256:" + strings.Repeat("a", 64) + "extra"},
		{name: "leading text before the prefix", value: "x sha256:" + strings.Repeat("a", 64)},
		{name: "embedded newline after a valid digest", value: "sha256:" + strings.Repeat("a", 64) + "\nsha256:x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := container.ValidDigest(tc.value); got != tc.want {
				t.Errorf("ValidDigest(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// TestCanonicalImageRef_CanonicalisesTheSuccessPaths pairs the refusal table
// with the outputs it produces when it accepts.
//
// CanonicalImageRef had refusal cases only, so what it DOES was untested: the
// docker:// transport strip and, more importantly, dropping the tag in front of
// a digest. That second one is not cosmetic. Buildah and Skopeo reject
// name:tag@sha256:... and the whole reason this function exists is to hand them
// the digest form, so a change that stopped dropping the tag would break every
// digest-pinned pull while every existing test kept passing.
func TestCanonicalImageRef_CanonicalisesTheSuccessPaths(t *testing.T) {
	t.Parallel()

	digest := "sha256:" + strings.Repeat("a", 64)

	for _, tc := range []struct {
		name, in, want, why string
	}{
		{
			name: "a plain tagged ref is unchanged",
			in:   "ghcr.io/org/app:v1.2.3", want: "ghcr.io/org/app:v1.2.3",
		},
		{
			name: "a digest ref is unchanged",
			in:   "ghcr.io/org/app@" + digest, want: "ghcr.io/org/app@" + digest,
		},
		{
			name: "a tag in front of a digest is dropped",
			in:   "ghcr.io/org/app:v1.2.3@" + digest, want: "ghcr.io/org/app@" + digest,
			why: "buildah and skopeo refuse name:tag@digest",
		},
		{
			name: "the docker transport is stripped",
			in:   "docker://ghcr.io/org/app:v1.2.3", want: "ghcr.io/org/app:v1.2.3",
		},
		{
			name: "the docker transport is stripped from a digest ref too",
			in:   "docker://ghcr.io/org/app:v1.2.3@" + digest, want: "ghcr.io/org/app@" + digest,
		},
		{
			name: "a registry port survives",
			in:   "localhost:5000/org/app:v1.2.3", want: "localhost:5000/org/app:v1.2.3",
			why: "the port colon must not be mistaken for a tag separator",
		},
		{
			name: "a registry port survives alongside a dropped tag",
			in:   "localhost:5000/org/app:v1.2.3@" + digest, want: "localhost:5000/org/app@" + digest,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := container.CanonicalImageRef(tc.in)
			if err != nil {
				t.Fatalf("CanonicalImageRef(%q) errored: %v", tc.in, err)
			}

			if got != tc.want {
				t.Errorf("CanonicalImageRef(%q) = %q, want %q: %s", tc.in, got, tc.want, tc.why)
			}
		})
	}
}
