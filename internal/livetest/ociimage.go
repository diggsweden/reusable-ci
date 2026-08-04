// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// The image-ledger scenarios need images in a real registry. They do not need
// *built* images.
//
// What the ledger records and verifies is refs, digests and tags: it copies a
// candidate tag to a final tag and re-checks that the digest the registry
// serves is the one recorded. Layer content never enters that, so a synthetic
// image exercises the whole surface in seconds. Building one with buildah would
// add minutes per scenario, a toolchain dependency this tier deliberately has
// none of, and no additional coverage of the thing under test.
//
// That is a claim about *this* tier. Whether a Containerfile builds, whether the
// artifact is reproducible, and whether a consumer's template produces a
// pushable multi-arch image are all real questions — they belong to the
// black-box suite and to the consumer-surface tier, where realism is the point
// rather than an expense.
//
// The push goes through go-containerregistry, the same library the product's
// own registry adapter uses, for one reason that matters more than convenience:
// it performs the real registry auth handshake (WWW-Authenticate challenge,
// token endpoint, Bearer). That handshake is exactly where forges differ, and
// hand-rolling it here would mean any mistake surfaced as a finding about the
// forge rather than a bug in the test.

// Image is a synthetic image pushed to a lab registry.
type Image struct {
	// Ref is the full tagged reference, e.g.
	// registry.gitlab.compose.gitproviderlab:8443/garga/rc-ledger:staging-v0.0.1
	Ref string

	// Digest is what the registry reports for it, in sha256:... form. This is
	// the value a ledger entry records and every later verification compares.
	Digest string

	// Index reports whether a multi-arch index was pushed rather than a single
	// image. Promotion is most interesting for an index, where a copy that does
	// not preserve the digest lands a different manifest.
	Index bool
}

// RegistryHost is where a forge serves its OCI registry. GitLab runs a separate
// registry host; the Gitea family serves packages from the forge host itself.
func RegistryHost(target Target) (string, error) {
	switch target.Kind {
	case provider.PlatformGitLab:
		return "registry." + target.Host, nil
	case provider.PlatformForgejo:
		return target.Host, nil
	case provider.PlatformGitHub, provider.PlatformLocal:
	}

	return "", fmt.Errorf("no lab registry host for platform %q: %w", target.Kind, errs.ErrUnsupported)
}

// PushImage puts a synthetic single-arch image at owner/repo:tag and returns
// what the registry then serves for it.
func PushImage(tb TB, target Target, repo, tag string) Image {
	tb.Helper()

	image, err := random.Image(1024, 1)
	if err != nil {
		tb.Fatalf("livetest: build synthetic image: %v", err)
	}

	return pushArtifact(tb, target, repo, tag, image, false)
}

// PushIndex puts a synthetic multi-arch index at owner/repo:tag. Real releases
// publish indexes, and digest-preserving promotion is where a naive copy breaks
// them, so the ledger scenarios run against both shapes.
func PushIndex(tb TB, target Target, repo, tag string) Image {
	tb.Helper()

	index, err := random.Index(1024, 1, 2)
	if err != nil {
		tb.Fatalf("livetest: build synthetic index: %v", err)
	}

	return pushArtifact(tb, target, repo, tag, index, true)
}

// pushable is the shared shape of an image and an index: both can be written to
// a registry and both can report the digest the registry will serve.
type pushable interface {
	Digest() (v1.Hash, error)
}

func pushArtifact(tb TB, target Target, repo, tag string, artifact pushable, isIndex bool) Image {
	tb.Helper()
	requireAccepted(tb, target)

	host, err := RegistryHost(target)
	if err != nil {
		tb.Fatalf("livetest: %v", err)
	}

	// The repository name carries this suite's namespace because it is derived
	// from the scratch repo, so a stray push cannot land outside what the guard
	// authorised.
	if !strings.HasPrefix(repo, ResourcePrefix) {
		tb.Fatalf("livetest: refusing to push to %q, outside the %q namespace", repo, ResourcePrefix)
	}

	reference, err := name.NewTag(fmt.Sprintf("%s/%s/%s:%s", host, target.Owner, repo, tag))
	if err != nil {
		tb.Fatalf("livetest: parse registry reference: %v", err)
	}

	auth := remote.WithAuth(&authn.Basic{Username: target.Owner, Password: target.Token})

	switch typed := artifact.(type) {
	case v1.Image:
		err = remote.Write(reference, typed, auth)
	case v1.ImageIndex:
		err = remote.WriteIndex(reference, typed, auth)
	default:
		tb.Fatalf("livetest: unsupported artifact type %T", artifact)
	}

	if err != nil {
		tb.Fatalf("livetest: push %s: %v", reference, err)
	}

	// Read the digest back from the registry rather than trusting the local
	// computation: what every later verification compares against is what the
	// registry serves, and those are only the same thing if the push landed.
	descriptor, err := remote.Head(reference, auth)
	if err != nil {
		tb.Fatalf("livetest: read back %s: %v", reference, err)
	}

	return Image{Ref: reference.String(), Digest: descriptor.Digest.String(), Index: isIndex}
}

// ImageDigest asks the registry what it currently serves for a tag. Absent is
// ("", false, nil): a tag that is gone is an answer scenarios assert on.
func ImageDigest(tb TB, target Target, repo, tag string) (string, bool) {
	tb.Helper()

	host, err := RegistryHost(target)
	if err != nil {
		tb.Fatalf("livetest: %v", err)
	}

	reference, err := name.NewTag(fmt.Sprintf("%s/%s/%s:%s", host, target.Owner, repo, tag))
	if err != nil {
		tb.Fatalf("livetest: parse registry reference: %v", err)
	}

	descriptor, err := remote.Head(reference, remote.WithAuth(&authn.Basic{
		Username: target.Owner,
		Password: target.Token,
	}))
	if err != nil {
		if strings.Contains(err.Error(), "MANIFEST_UNKNOWN") || strings.Contains(err.Error(), "NAME_UNKNOWN") ||
			strings.Contains(err.Error(), "404") {
			return "", false
		}

		tb.Fatalf("livetest: head %s: %v", reference, err)
	}

	return descriptor.Digest.String(), true
}
