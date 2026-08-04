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
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/docker/cli/cli/config"
	dockertypes "github.com/docker/cli/cli/config/types"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/httpretry"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// osLinux is the only image platform OS reusable-ci selects from an index.
const osLinux = "linux"

// Adapter resolves, copies, and assembles image manifests over the registry
// API. It satisfies imageledger.Registry and the manifest-helper registry
// surface in internal/app/container.
type Adapter struct {
	AuthFile string
}

// New returns an Adapter authenticating from the default keychain.
func New() *Adapter { return &Adapter{} }

// WithAuthFile returns an Adapter that authenticates from a Docker-compatible
// config file instead of the process-default keychain.
func WithAuthFile(path string) *Adapter { return &Adapter{AuthFile: path} }

// ResolveDigest returns the sha256 digest the registry currently serves for
// ref (a tag or digest-pinned ref). For a multi-arch tag this is the index
// digest — the same value `docker buildx imagetools inspect
// --format {{.Manifest.Digest}}` reports.
func (a *Adapter) ResolveDigest(ctx context.Context, ref string) (string, error) {
	digest, err := crane.Digest(ref, a.craneOpts(ctx, ref)...)
	if err != nil {
		return "", fmt.Errorf("resolve digest for %s: %w: %w", ref, err, classifyRegistryError(err))
	}

	if digest = strings.TrimSpace(digest); digest == "" {
		return "", fmt.Errorf("resolve digest for %s: registry returned no digest: %w", ref, errs.ErrDependencyUnavailable)
	}

	return digest, nil
}

// classifyRegistryError maps a registry library error onto a domain sentinel by
// its HTTP status, falling back to "the dependency is unavailable" only when the
// error carries no status at all.
//
// Every registry call routes through here rather than asserting unavailability,
// because the two are not interchangeable to a caller: unavailable means "try
// again", and CI does. A permanent condition reported that way — a manifest that
// does not exist, a credential that is refused — becomes a retry loop that can
// never succeed. This is the same defect the token path had (PAR-TOK-2); it was
// found again here when a rollback on a forge that drops untagged manifests
// exited 69 instead of saying the image was gone.
func classifyRegistryError(err error) error {
	var transportErr *transport.Error
	if errors.As(err, &transportErr) {
		if transportErr.StatusCode == http.StatusNotFound {
			return errs.ErrMissingInput
		}

		if mapped := errs.FromHTTPStatus(transportErr.StatusCode); mapped != nil {
			return mapped
		}
	}

	return errs.ErrDependencyUnavailable
}

// CopyTag points dest at the manifest (image or multi-arch index) currently
// served for source, preserving the digest. Within one repository this is a
// tag write of the shared digest; across repositories or registries it copies
// the manifest and mounts/uploads the referenced blobs — the same outcome as
// `docker buildx imagetools create --tag dest source`, without a daemon.
func (a *Adapter) CopyTag(ctx context.Context, source, dest string) error {
	if err := crane.Copy(source, dest, a.craneOpts(ctx, source, dest)...); err != nil {
		return fmt.Errorf("retag %s -> %s: %w: %w", source, dest, err, classifyRegistryError(err))
	}

	return nil
}

// PushLayoutByDigest reads the single image from an OCI image layout on disk
// (the artifact buildah exports after a build) and writes it to imageRef's
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
		return "", fmt.Errorf("compute image digest: %w: %w", err, classifyRegistryError(err))
	}

	base, err := a.parse(imageRef)
	if err != nil {
		return "", err
	}

	// Context().Digest drops any tag and keeps the registry's insecure flag, so
	// the write targets <repo>@<digest> over the same (HTTP-for-loopback) transport.
	ref := base.Context().Digest(dig.String())
	if err := remote.Write(ref, img, a.remoteOpts(ctx)...); err != nil {
		return "", fmt.Errorf("push image by digest to %s: %w: %w", base.Context().Name(), err, classifyRegistryError(err))
	}

	return dig.String(), nil
}

// Manifest returns the raw manifest document the registry serves for ref (an
// image manifest or a multi-arch index), for human-readable inspection — the
// daemonless counterpart of `docker buildx imagetools inspect`.
func (a *Adapter) Manifest(ctx context.Context, ref string) ([]byte, error) {
	raw, err := crane.Manifest(ref, a.craneOpts(ctx, ref)...)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest for %s: %w: %w", ref, err, classifyRegistryError(err))
	}

	return raw, nil
}

// Labels returns the config labels for the image selected by ref. If ref points
// at a multi-platform index, the host platform is selected, matching the
// inspection behavior the shell migration previously relied on.
func (a *Adapter) Labels(ctx context.Context, ref string) (map[string]string, error) {
	parsed, err := a.parse(ref)
	if err != nil {
		return nil, fmt.Errorf("parse image ref %s: %w", ref, err)
	}

	opts := append(a.remoteOpts(ctx), remote.WithPlatform(v1.Platform{OS: runtime.GOOS, Architecture: runtime.GOARCH}))

	desc, err := remote.Get(parsed, opts...)
	if err != nil {
		return nil, fmt.Errorf("fetch image %s: %w: %w", ref, err, classifyRegistryError(err))
	}

	img, err := desc.Image()
	if err != nil {
		return nil, fmt.Errorf("read image %s: %w: %w", ref, err, classifyRegistryError(err))
	}

	config, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("read image config %s: %w: %w", ref, err, classifyRegistryError(err))
	}

	labels := make(map[string]string, len(config.Config.Labels))
	for key, value := range config.Config.Labels {
		labels[key] = value
	}

	return labels, nil
}

// InspectImage returns the digest, architecture, and config labels for ref. If
// ref points at an index, the descriptor matching arch is selected.
func (a *Adapter) InspectImage(ctx context.Context, ref, arch string) (string, string, map[string]string, error) {
	parsed, err := a.parse(ref)
	if err != nil {
		return "", "", nil, fmt.Errorf("parse image ref %s: %w", ref, err)
	}

	desc, err := remote.Get(parsed, a.remoteOpts(ctx)...)
	if err != nil {
		return "", "", nil, fmt.Errorf("fetch image %s: %w: %w", ref, err, classifyRegistryError(err))
	}

	if desc.MediaType.IsIndex() {
		return a.inspectImageIndex(ref, desc, arch)
	}

	img, err := desc.Image()
	if err != nil {
		return "", "", nil, fmt.Errorf("read image %s: %w: %w", ref, err, classifyRegistryError(err))
	}

	digest := desc.Digest.String()
	if digest == "" {
		computed, digestErr := img.Digest()
		if digestErr != nil {
			return "", "", nil, fmt.Errorf("compute image digest %s: %w: %w", ref, digestErr, classifyRegistryError(digestErr))
		}

		digest = computed.String()
	}

	architecture, labels, err := imageConfigMetadata(img, ref)
	if err != nil {
		return "", "", nil, err
	}

	return digest, architecture, labels, nil
}

// findLinuxArchDescriptor returns the single linux/<arch> child descriptor of
// an image index, rejecting missing or ambiguous entries.
func findLinuxArchDescriptor(manifest *v1.IndexManifest, ref, arch string) (*v1.Descriptor, error) {
	var match *v1.Descriptor

	for _, child := range manifest.Manifests {
		if child.Platform == nil || child.Platform.Architecture != arch {
			continue
		}

		if child.Platform.OS != "" && child.Platform.OS != osLinux {
			continue
		}

		if match != nil {
			return nil, fmt.Errorf("image index %s has multiple linux/%s entries: %w", ref, arch, errs.ErrValidation)
		}

		selected := child
		match = &selected
	}

	if match == nil {
		return nil, fmt.Errorf("image index %s has no linux/%s entry: %w", ref, arch, errs.ErrMissingInput)
	}

	return match, nil
}

func imageConfigMetadata(img v1.Image, ref string) (string, map[string]string, error) {
	config, err := img.ConfigFile()
	if err != nil {
		return "", nil, fmt.Errorf("read image config %s: %w: %w", ref, err, classifyRegistryError(err))
	}

	labels := make(map[string]string, len(config.Config.Labels))
	for key, value := range config.Config.Labels {
		labels[key] = value
	}

	return config.Architecture, labels, nil
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
			return fmt.Errorf("merge manifest: write index to %s: %w: %w", tag, err, classifyRegistryError(err))
		}
	}

	return nil
}

func (a *Adapter) inspectImageIndex(ref string, desc *remote.Descriptor, arch string) (string, string, map[string]string, error) {
	idx, err := desc.ImageIndex()
	if err != nil {
		return "", "", nil, fmt.Errorf("read image index %s: %w: %w", ref, err, classifyRegistryError(err))
	}

	manifest, err := idx.IndexManifest()
	if err != nil {
		return "", "", nil, fmt.Errorf("read image index manifest %s: %w: %w", ref, err, classifyRegistryError(err))
	}

	match, err := findLinuxArchDescriptor(manifest, ref, arch)
	if err != nil {
		return "", "", nil, err
	}

	img, err := idx.Image(match.Digest)
	if err != nil {
		return "", "", nil, fmt.Errorf("read image %s child %s: %w: %w", ref, match.Digest, err, classifyRegistryError(err))
	}

	architecture, labels, err := imageConfigMetadata(img, ref)
	if err != nil {
		return "", "", nil, err
	}

	if architecture == "" {
		architecture = match.Platform.Architecture
	}

	return match.Digest.String(), architecture, labels, nil
}

// addendaFor resolves one source digest into the index entries to merge. A
// plain image → one entry (platform from its config). A BuildKit index → one
// entry per child manifest (the image and each attestation), preserving the
// child's platform and annotations so the provenance/SBOM linkage survives the
// merge.
func (a *Adapter) addendaFor(ctx context.Context, ref name.Reference) ([]mutate.IndexAddendum, error) {
	desc, err := remote.Get(ref, a.remoteOpts(ctx)...)
	if err != nil {
		return nil, fmt.Errorf("merge manifest: fetch %s: %w: %w", ref, err, classifyRegistryError(err))
	}

	if !desc.MediaType.IsIndex() {
		return singleImageAddendum(ref, desc)
	}

	idx, err := desc.ImageIndex()
	if err != nil {
		return nil, fmt.Errorf("merge manifest: read index %s: %w: %w", ref, err, classifyRegistryError(err))
	}

	manifest, err := idx.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("merge manifest: read index manifest %s: %w: %w", ref, err, classifyRegistryError(err))
	}

	addenda := make([]mutate.IndexAddendum, 0, len(manifest.Manifests))

	for _, child := range manifest.Manifests {
		img, err := idx.Image(child.Digest)
		if err != nil {
			return nil, fmt.Errorf("merge manifest: read child %s of %s: %w: %w", child.Digest, ref, err, classifyRegistryError(err))
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
		return nil, fmt.Errorf("merge manifest: read image %s: %w: %w", ref, err, classifyRegistryError(err))
	}

	config, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("merge manifest: read config of %s: %w: %w", ref, err, classifyRegistryError(err))
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
		remote.WithAuthFromKeychain(a.keychain()),
		remote.WithTransport(httpretry.NewTransport(httpretry.Config{})),
	}
}

// craneOpts builds crane options: cancellable context + keychain auth, plus
// insecure transport when every ref targets a loopback host.
func (a *Adapter) craneOpts(ctx context.Context, refs ...string) []crane.Option {
	opts := []crane.Option{
		crane.WithContext(ctx),
		crane.WithAuthFromKeychain(a.keychain()),
		crane.WithTransport(httpretry.NewTransport(httpretry.Config{})),
	}

	if allLoopback(refs) {
		opts = append(opts, crane.Insecure)
	}

	return opts
}

func (a *Adapter) keychain() authn.Keychain {
	if a.AuthFile == "" {
		return authn.DefaultKeychain
	}

	return authFileKeychain{path: a.AuthFile}
}

type authFileKeychain struct {
	path string
}

func (k authFileKeychain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	authFile, err := os.Open(k.path) //nolint:gosec // caller-selected Docker auth config.
	if err != nil {
		return nil, fmt.Errorf("open registry auth file %s: %w: %w", k.path, err, errs.ErrMissingInput)
	}

	defer func() { _ = authFile.Close() }()

	cf, err := config.LoadFromReader(authFile)
	if err != nil {
		return nil, fmt.Errorf("load registry auth file %s: %w: %w", k.path, err, errs.ErrMalformedInput)
	}

	cfg, err := dockerAuthConfig(cf, target)
	if err != nil {
		return nil, err
	}

	var empty dockertypes.AuthConfig
	if cfg == empty {
		return authn.Anonymous, nil
	}

	return authn.FromConfig(authn.AuthConfig{
		Username:      cfg.Username,
		Password:      cfg.Password,
		Auth:          cfg.Auth,
		IdentityToken: cfg.IdentityToken,
		RegistryToken: cfg.RegistryToken,
	}), nil
}

func dockerAuthConfig(cf interface {
	GetAuthConfig(registryKey string) (dockertypes.AuthConfig, error)
}, target authn.Resource) (dockertypes.AuthConfig, error) {
	var empty dockertypes.AuthConfig

	for _, key := range []string{target.String(), target.RegistryStr()} {
		if key == name.DefaultRegistry {
			key = authn.DefaultAuthKey
		}

		cfg, err := cf.GetAuthConfig(key)
		if err != nil {
			return empty, err
		}

		cfg.ServerAddress = ""
		if cfg != empty {
			return cfg, nil
		}
	}

	return empty, nil
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
