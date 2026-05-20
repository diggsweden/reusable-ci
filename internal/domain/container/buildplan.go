// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// BuildOutputMode selects what `container build` does with the built image.
// The three modes are mutually exclusive by construction (a single field),
// mirroring the docker/build-push-action output shapes reusable-ci uses.
type BuildOutputMode string

// Supported build output modes.
const (
	// BuildModePushByDigest pushes the per-arch image and returns its digest,
	// with no human tag of its own — the multi-arch index and the real tags are
	// applied later by ggcr `container manifest merge`. Mirrors build-push's
	// outputs=type=image,push-by-digest=true.
	BuildModePushByDigest BuildOutputMode = "push-by-digest"
	// BuildModeLoad builds into local container storage under a tag, for
	// smoke-testing (the self-runtime-container flow's load:true). No push.
	BuildModeLoad BuildOutputMode = "load"
	// BuildModeLocal exports a build stage's filesystem to a directory, for
	// binary extraction (build-push's outputs=type=local). No image, no push.
	BuildModeLocal BuildOutputMode = "local"
)

// BuildRequest is the tool-neutral description of one image build. The buildah
// adapter translates it to argv. The fields are the subset of
// docker/build-push-action reusable-ci actually uses: a SINGLE native platform
// (the multi-arch index is assembled later by ggcr, so there is no QEMU
// cross-build here), secrets pre-materialized to 0600 tmpfiles, and a
// forge-neutral registry layer cache instead of the GitHub-specific type=gha.
type BuildRequest struct {
	Context       string // build context directory (required)
	Containerfile string // path to the Containerfile/Dockerfile; empty → buildah's default lookup
	Target        string // multi-stage target stage; empty → final stage
	Platform      string // single platform, e.g. linux/arm64; empty → host

	BuildArgs []string // "KEY=VALUE" pairs
	Secrets   []string // "id=NAME,src=PATH" — the buildah/build-push format, emitted verbatim by `container materialize-build-secrets`, passed straight through
	Labels    []string // "key=value" OCI labels (from `container metadata`)

	SourceDateEpoch string // unix seconds; empty disables the reproducibility timestamp clamp

	// Forge-neutral registry layer cache (replacing the GitHub-specific type=gha).
	// CacheRepo is the dedicated cache repository; CacheScope tag-scopes it per
	// build. The single convention <CacheRepo>:<CacheScope> is assembled by
	// CacheRef so callers never re-encode it. Import is best-effort (a miss just
	// builds from scratch); CachePush also EXPORTS and is set only on a trusted
	// push (never a fork PR). Empty CacheRepo disables caching entirely.
	CacheRepo  string
	CacheScope string
	CachePush  bool

	Mode      BuildOutputMode
	ImageRef  string // push target (push-by-digest) or local tag (load)
	OutputDir string // destination directory (local)
}

// Validate checks mode-specific required fields and secret shape. The single
// Mode field already makes the output modes mutually exclusive.
func (r BuildRequest) Validate() error {
	if r.Context == "" {
		return fmt.Errorf("build context is required: %w", errs.ErrUsage)
	}

	switch r.Mode {
	case BuildModePushByDigest, BuildModeLoad:
		if r.ImageRef == "" {
			return fmt.Errorf("image reference is required for %s mode: %w", r.Mode, errs.ErrUsage)
		}
	case BuildModeLocal:
		if r.OutputDir == "" {
			return fmt.Errorf("output directory is required for local mode: %w", errs.ErrUsage)
		}
	default:
		return fmt.Errorf("unknown build output mode %q (want push-by-digest|load|local): %w", r.Mode, errs.ErrUsage)
	}

	for _, s := range r.Secrets {
		if !strings.HasPrefix(s, "id=") {
			return fmt.Errorf("malformed build secret %q (want id=NAME,src=PATH): %w", s, errs.ErrUsage)
		}
	}

	return nil
}

// CacheRef is the single forge-neutral cache convention: <CacheRepo>:<CacheScope>
// (or the bare repo when no scope). Empty CacheRepo → "" (caching disabled).
// Centralised here so no workflow re-encodes the join.
func (r BuildRequest) CacheRef() string {
	if r.CacheRepo == "" {
		return ""
	}

	if r.CacheScope == "" {
		return r.CacheRepo
	}

	return r.CacheRepo + ":" + r.CacheScope
}
