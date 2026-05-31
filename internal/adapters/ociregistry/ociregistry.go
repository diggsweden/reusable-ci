// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package ociregistry implements reusable-ci's registry-client operations —
// digest resolution, tag copy/move, manifest fetch, and multi-platform index
// assembly — directly over the OCI distribution HTTP API with
// go-containerregistry. It is daemonless: no docker CLI, no BuildKit, so ledger
// promotion/verification and manifest-list creation run in any runtime image
// without a container daemon.
//
// Auth comes from the standard keychain (the shared `~/.docker/config.json`
// that the `container login` verb writes), so a prior login authenticates
// these calls transparently — and because every operation is an in-process
// HTTP call rather than a subprocess, no credential ever reaches argv.
//
// Plain-HTTP (insecure) transport is enabled only when a reference targets a
// loopback host, matching docker/buildx's localhost handling: a loopback
// registry is never a real remote, so this keeps production registries on
// HTTPS while letting an in-process test registry serve over HTTP.
package ociregistry

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// Adapter resolves, copies, and assembles image manifests over the registry
// API. It satisfies imageledger.Registry and the manifest-helper registry
// surface in internal/app/container.
type Adapter struct{}

// New returns an Adapter authenticating from the default keychain.
func New() *Adapter { return &Adapter{} }

// ResolveDigest returns the sha256 digest the registry currently serves for
// ref (a tag or digest-pinned ref). For a multi-arch tag this is the index
// digest — the same value `docker buildx imagetools inspect
// --format {{.Manifest.Digest}}` reports.
func (a *Adapter) ResolveDigest(ctx context.Context, ref string) (string, error) {
	digest, err := crane.Digest(ref, a.craneOpts(ctx, ref)...)
	if err != nil {
		return "", fmt.Errorf("resolve digest for %s: %w: %w", ref, err, errs.ErrDependencyUnavailable)
	}

	if digest = strings.TrimSpace(digest); digest == "" {
		return "", fmt.Errorf("resolve digest for %s: registry returned no digest: %w", ref, errs.ErrDependencyUnavailable)
	}

	return digest, nil
}

// CopyTag points dest at the manifest (image or multi-arch index) currently
// served for source, preserving the digest. Within one repository this is a
// tag write of the shared digest; across repositories or registries it copies
// the manifest and mounts/uploads the referenced blobs — the same outcome as
// `docker buildx imagetools create --tag dest source`, without a daemon.
func (a *Adapter) CopyTag(ctx context.Context, source, dest string) error {
	if err := crane.Copy(source, dest, a.craneOpts(ctx, source, dest)...); err != nil {
		return fmt.Errorf("retag %s -> %s: %w: %w", source, dest, err, errs.ErrDependencyUnavailable)
	}

	return nil
}

// PushLayoutByDigest reads the single image from an OCI image layout on disk
// (the artefact buildah exports after a build) and writes it to imageRef's
// repository addressed by its OWN digest — a TAGLESS push (PUT manifest by
// digest), the daemonless equivalent of docker/build-push-action's
// push-by-digest=true. The per-arch manifest therefore exists only as an
// untagged version the multi-arch index references; there is no tag to create
// or clean up (removing a tag would delete the very manifest the index needs).
// Any tag on imageRef is ignored. Returns the pushed digest.
func (a *Adapter) PushLayoutByDigest(ctx context.Context, layoutDir, imageRef string) (string, error) {
	img, err := layoutImage(layoutDir)
	if err != nil {
		return "", err
	}

	dig, err := img.Digest()
	if err != nil {
		return "", fmt.Errorf("compute image digest: %w: %w", err, errs.ErrDependencyUnavailable)
	}

	base, err := a.parse(imageRef)
	if err != nil {
		return "", err
	}

	// Context().Digest drops any tag and keeps the registry's insecure flag, so
	// the write targets <repo>@<digest> over the same (HTTP-for-loopback) transport.
	ref := base.Context().Digest(dig.String())
	if err := remote.Write(ref, img, a.remoteOpts(ctx)...); err != nil {
		return "", fmt.Errorf("push image by digest to %s: %w: %w", base.Context().Name(), err, errs.ErrDependencyUnavailable)
	}

	return dig.String(), nil
}

// Manifest returns the raw manifest document the registry serves for ref (an
// image manifest or a multi-arch index), for human-readable inspection — the
// daemonless counterpart of `docker buildx imagetools inspect`.
func (a *Adapter) Manifest(ctx context.Context, ref string) ([]byte, error) {
	raw, err := crane.Manifest(ref, a.craneOpts(ctx, ref)...)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest for %s: %w: %w", ref, err, errs.ErrDependencyUnavailable)
	}

	return raw, nil
}

// MergeManifest assembles a multi-platform image index from the per-platform
// builds already pushed at image@sha256:<digest> and writes it to every tag —
// the daemonless equivalent of
// `docker buildx imagetools create -t <tag>... <image>@sha256:<digest>...`.
//
// Each source digest is either a plain image manifest OR a BuildKit index
// (the image plus its provenance/SBOM attestation manifests, produced by
// `--provenance`/`--sbom`). Both are handled: a plain image contributes one
// platform entry; an index contributes every child — the platform image and
// each `unknown/unknown` attestation manifest, with the
// `vnd.docker.reference.*` annotations preserved so SLSA provenance and the
// SBOM travel into the merged multi-arch index. No separate attestation API is
// used, so verification (cosign / slsa-verifier / `imagetools inspect`) is the
// same on GitHub, Forgejo and GitLab. The children already live in the
// repository, so only the index manifest is written.
func (a *Adapter) MergeManifest(ctx context.Context, image string, digests, tags []string) error {
	index := v1.ImageIndex(empty.Index)

	for _, digest := range digests {
		ref, err := a.parse(image + "@sha256:" + digest)
		if err != nil {
			return fmt.Errorf("merge manifest: parse source %s@sha256:%s: %w", image, digest, err)
		}

		addenda, err := a.addendaFor(ctx, ref)
		if err != nil {
			return err
		}

		index = mutate.AppendManifests(index, addenda...)
	}

	for _, tag := range tags {
		ref, err := a.parse(tag)
		if err != nil {
			return fmt.Errorf("merge manifest: parse tag %s: %w", tag, err)
		}

		if err := remote.WriteIndex(ref, index, a.remoteOpts(ctx)...); err != nil {
			return fmt.Errorf("merge manifest: write index to %s: %w: %w", tag, err, errs.ErrDependencyUnavailable)
		}
	}

	return nil
}

// addendaFor resolves one source digest into the index entries to merge. A
// plain image → one entry (platform from its config). A BuildKit index → one
// entry per child manifest (the image and each attestation), preserving the
// child's platform and annotations so the provenance/SBOM linkage survives the
// merge.
func (a *Adapter) addendaFor(ctx context.Context, ref name.Reference) ([]mutate.IndexAddendum, error) {
	desc, err := remote.Get(ref, a.remoteOpts(ctx)...)
	if err != nil {
		return nil, fmt.Errorf("merge manifest: fetch %s: %w: %w", ref, err, errs.ErrDependencyUnavailable)
	}

	if !desc.MediaType.IsIndex() {
		return singleImageAddendum(ref, desc)
	}

	idx, err := desc.ImageIndex()
	if err != nil {
		return nil, fmt.Errorf("merge manifest: read index %s: %w: %w", ref, err, errs.ErrDependencyUnavailable)
	}

	manifest, err := idx.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("merge manifest: read index manifest %s: %w: %w", ref, err, errs.ErrDependencyUnavailable)
	}

	addenda := make([]mutate.IndexAddendum, 0, len(manifest.Manifests))

	for _, child := range manifest.Manifests {
		img, err := idx.Image(child.Digest)
		if err != nil {
			return nil, fmt.Errorf("merge manifest: read child %s of %s: %w: %w", child.Digest, ref, err, errs.ErrDependencyUnavailable)
		}

		addenda = append(addenda, mutate.IndexAddendum{
			Add: img,
			Descriptor: v1.Descriptor{
				MediaType:   child.MediaType,
				Platform:    child.Platform,
				Annotations: child.Annotations,
			},
		})
	}

	return addenda, nil
}

// singleImageAddendum resolves a plain (non-index) image into a one-entry
// addendum carrying its platform. Split out of addendaFor so its err binding
// has its own scope (no shadowing of the caller's).
func singleImageAddendum(ref name.Reference, desc *remote.Descriptor) ([]mutate.IndexAddendum, error) {
	img, err := desc.Image()
	if err != nil {
		return nil, fmt.Errorf("merge manifest: read image %s: %w: %w", ref, err, errs.ErrDependencyUnavailable)
	}

	config, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("merge manifest: read config of %s: %w: %w", ref, err, errs.ErrDependencyUnavailable)
	}

	return []mutate.IndexAddendum{{Add: img, Descriptor: v1.Descriptor{Platform: config.Platform()}}}, nil
}

// layoutImage loads the single image from an OCI image layout directory.
func layoutImage(dir string) (v1.Image, error) {
	idx, err := layout.ImageIndexFromPath(dir)
	if err != nil {
		return nil, fmt.Errorf("read oci layout %s: %w: %w", dir, err, errs.ErrMalformedInput)
	}

	manifest, err := idx.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("read oci index in %s: %w: %w", dir, err, errs.ErrMalformedInput)
	}

	if len(manifest.Manifests) == 0 {
		return nil, fmt.Errorf("oci layout %s contains no image: %w", dir, errs.ErrMalformedInput)
	}

	img, err := idx.Image(manifest.Manifests[0].Digest)
	if err != nil {
		return nil, fmt.Errorf("load image from layout %s: %w: %w", dir, err, errs.ErrMalformedInput)
	}

	return img, nil
}

// parse resolves a reference, marking it insecure (plain HTTP) when it targets
// a loopback host (see the package doc).
func (a *Adapter) parse(ref string) (name.Reference, error) {
	var opts []name.Option
	if loopbackRef(ref) {
		opts = append(opts, name.Insecure)
	}

	return name.ParseReference(ref, opts...)
}

func (a *Adapter) remoteOpts(ctx context.Context) []remote.Option {
	return []remote.Option{
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	}
}

// craneOpts builds crane options: cancellable context + keychain auth, plus
// insecure transport when every ref targets a loopback host.
func (a *Adapter) craneOpts(ctx context.Context, refs ...string) []crane.Option {
	opts := []crane.Option{
		crane.WithContext(ctx),
		crane.WithAuthFromKeychain(authn.DefaultKeychain),
	}

	if allLoopback(refs) {
		opts = append(opts, crane.Insecure)
	}

	return opts
}

func allLoopback(refs []string) bool {
	if len(refs) == 0 {
		return false
	}

	for _, ref := range refs {
		if !loopbackRef(ref) {
			return false
		}
	}

	return true
}

func loopbackRef(ref string) bool {
	parsed, err := name.ParseReference(ref)
	if err != nil {
		return false
	}

	return isLoopbackHost(parsed.Context().RegistryStr())
}

func isLoopbackHost(registry string) bool {
	host := registry
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}

	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
