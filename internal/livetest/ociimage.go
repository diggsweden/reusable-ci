// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	switch target.Forge {
	case provider.ForgeGitLab:
		return "registry." + target.Host, nil
	case provider.ForgeForgejo:
		return target.Host, nil
	case provider.ForgeGitHub, provider.ForgeLocal:
	}

	return "", fmt.Errorf("no lab registry host for platform %q: %w", target.Forge, errs.ErrUnsupported)
}

// PushImage puts a synthetic single-arch image at owner/repo:tag and returns
// what the registry then serves for it.
func PushImage(tb TB, target Target, repo, tag string) Image {
	return PushImageTags(tb, target, repo, tag)
}

// PushImageTags puts ONE synthetic image at several tags, which is how a build
// really leaves the registry: the immutable :<version> tag and its staging
// candidate are the same manifest, and that shared digest is the whole reason
// cleanup must delete tags rather than manifests. Pushing twice would mint two
// digests and quietly turn that property into something the test cannot see.
func PushImageTags(tb TB, target Target, repo string, tags ...string) Image {
	tb.Helper()

	image, err := random.Image(1024, 1)
	if err != nil {
		tb.Fatalf("livetest: build synthetic image: %v", err)
	}

	return pushArtifact(tb, target, repo, tags, image, false)
}

// PushIndex puts a synthetic multi-arch index at owner/repo:tag. Real releases
// publish indexes, and digest-preserving promotion is where a naive copy breaks
// them, so the ledger scenarios run against both shapes.
func PushIndex(tb TB, target Target, repo, tag string) Image {
	return PushIndexTags(tb, target, repo, tag)
}

// PushIndexTags is PushImageTags for a multi-arch index.
func PushIndexTags(tb TB, target Target, repo string, tags ...string) Image {
	tb.Helper()

	index, err := random.Index(1024, 1, 2)
	if err != nil {
		tb.Fatalf("livetest: build synthetic index: %v", err)
	}

	return pushArtifact(tb, target, repo, tags, index, true)
}

// pushable is the shared shape of an image and an index: both can be written to
// a registry and both can report the digest the registry will serve.
type pushable interface {
	Digest() (v1.Hash, error)
}

func pushArtifact(tb TB, target Target, repo string, tags []string, artifact pushable, isIndex bool) Image {
	tb.Helper()
	requireAccepted(tb, target)

	if len(tags) == 0 {
		tb.Fatalf("livetest: push needs at least one tag")
	}

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

	auth := remote.WithAuth(&authn.Basic{Username: target.Owner, Password: target.Token})

	var reference name.Tag

	for _, tag := range tags {
		reference = writeArtifact(tb, fmt.Sprintf("%s/%s/%s:%s", host, target.Owner, repo, tag), artifact, auth)
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

// writeArtifact pushes one image or index to a single tag and returns the
// parsed reference, so pushArtifact's loop stays about the tag set rather than
// about the two artifact shapes.
func writeArtifact(tb TB, ref string, artifact pushable, auth remote.Option) name.Tag {
	tb.Helper()

	reference, err := name.NewTag(ref)
	if err != nil {
		tb.Fatalf("livetest: parse registry reference: %v", err)
	}

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

	return reference
}

// RegistryAuthFile writes a Docker auth config for the target's registry and
// returns its path, for the verbs that reach a registry in-process.
//
// The product resolves registry credentials from
// $REUSABLE_CI_REGISTRY_AUTH_FILE, falling back to the ambient Docker keychain.
// A scenario must supply the file rather than lean on that fallback: the
// closed environment CLI runs in has no ambient keychain by design, and a test
// that quietly depended on the operator's ~/.docker/config.json would pass or
// fail based on who ran it.
func RegistryAuthFile(tb TB, target Target, dir string) string {
	tb.Helper()

	host, err := RegistryHost(target)
	if err != nil {
		tb.Fatalf("livetest: %v", err)
	}

	credential := base64.StdEncoding.EncodeToString([]byte(target.Owner + ":" + target.Token))

	config := map[string]any{
		"auths": map[string]any{
			host: map[string]string{"auth": credential},
		},
	}

	encoded, err := json.Marshal(config)
	if err != nil {
		tb.Fatalf("livetest: encode registry auth: %v", err)
	}

	path := filepath.Join(dir, "registry-auth.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		tb.Fatalf("livetest: write registry auth: %v", err)
	}

	return path
}

// RegistryAuthConfigDir writes the same credentials as RegistryAuthFile, but as
// config.json inside a directory, which is the shape $DOCKER_CONFIG names and
// therefore what the cosign subprocess can read. Returns the directory.
func RegistryAuthConfigDir(tb TB, target Target, dir string) string {
	tb.Helper()

	configDir := filepath.Join(dir, "docker")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		tb.Fatalf("livetest: create docker config dir: %v", err)
	}

	written := RegistryAuthFile(tb, target, configDir)
	if err := os.Rename(written, filepath.Join(configDir, "config.json")); err != nil {
		tb.Fatalf("livetest: place docker config: %v", err)
	}

	return configDir
}

// SignaturePublishedToTransparencyLog reports whether the cosign signature
// attached to an image digest carries a transparency-log entry.
//
// It reads the signature manifest straight from the registry rather than asking
// cosign, for a reason that is not about speed: `cosign verify` without
// --insecure-ignore-tlog fetches Sigstore's trust root from
// tuf-repo-cdn.sigstore.dev, so using verification to prove "we published
// nothing" makes an outbound connection to Sigstore on every run — the exact
// contact the containment exists to avoid. This asks the registry a question it
// can answer locally.
//
// cosign stores a Rekor entry as the dev.sigstore.cosign/bundle annotation on
// the signature layer. Absent annotation, nothing was published.
func SignaturePublishedToTransparencyLog(tb TB, target Target, repo, digest string) bool {
	tb.Helper()

	host, err := RegistryHost(target)
	if err != nil {
		tb.Fatalf("livetest: %v", err)
	}

	// cosign 3.x attaches the signature at the digest-derived tag sha256-<hex>
	// (the OCI 1.1 referrers fallback scheme). cosign 2.x used a ".sig" suffix;
	// this was confirmed against the registry rather than assumed, because the
	// two schemes fail in opposite ways — guessing the old one made this look
	// like "no signature" when there was one.
	signatureTag := strings.Replace(digest, ":", "-", 1)

	reference, err := name.NewTag(fmt.Sprintf("%s/%s/%s:%s", host, target.Owner, repo, signatureTag))
	if err != nil {
		tb.Fatalf("livetest: parse signature reference: %v", err)
	}

	image, err := remote.Image(reference, remote.WithAuth(&authn.Basic{
		Username: target.Owner, Password: target.Token,
	}))
	if err != nil {
		tb.Fatalf("livetest: read signature manifest %s: %v", reference, err)
	}

	manifest, err := image.Manifest()
	if err != nil {
		tb.Fatalf("livetest: decode signature manifest %s: %v", reference, err)
	}

	for _, layer := range manifest.Layers {
		if _, published := layer.Annotations["dev.sigstore.cosign/bundle"]; published {
			return true
		}
	}

	return false
}

// RegistrySnapshot records every tag in a repository and the digest it serves.
//
// It exists for the dry-run scenarios, where the claim is "the registry is
// exactly as it was". Comparing whole snapshots rather than the tags a verb was
// expected to touch is the point: a dry-run that quietly wrote some OTHER tag
// would pass a narrower check, and the promise is that nothing was mutated, not
// that the predicted mutation was skipped.
func RegistrySnapshot(tb TB, target Target, repo string) map[string]string {
	tb.Helper()

	host, err := RegistryHost(target)
	if err != nil {
		tb.Fatalf("livetest: %v", err)
	}

	auth := remote.WithAuth(&authn.Basic{Username: target.Owner, Password: target.Token})

	repository, err := name.NewRepository(fmt.Sprintf("%s/%s/%s", host, target.Owner, repo))
	if err != nil {
		tb.Fatalf("livetest: parse repository: %v", err)
	}

	tags, err := remote.List(repository, auth)
	if err != nil {
		// A repository with nothing pushed yet is an empty snapshot, not a
		// failure: a scenario may snapshot before its first push.
		if strings.Contains(err.Error(), "NAME_UNKNOWN") || strings.Contains(err.Error(), "404") {
			return map[string]string{}
		}

		tb.Fatalf("livetest: list tags for %s: %v", repo, err)
	}

	snapshot := make(map[string]string, len(tags))

	for _, tag := range tags {
		digest, found := ImageDigest(tb, target, repo, tag)
		if !found {
			continue
		}

		snapshot[tag] = digest
	}

	return snapshot
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
