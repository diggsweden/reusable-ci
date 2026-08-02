// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger

import (
	"fmt"
	"regexp"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// stageNameRE matches a stage name safe to use as the tag component of a
// promoted ref (<base>:<name>) — the OCI tag charset. A release stage
// (empty name) is exempt; it uses the entry's existing tags verbatim.
//
//nolint:gochecknoglobals // compiled regex — read-only.
var stageNameRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)

// Stage names a promotion target and computes the destination tags the
// recorded digest is retagged to for that stage. It encodes the
// build-once / promote-many tag scheme: every stage tag points at the same
// immutable digest (@sha256:…), so promotion is a verified tag-move, never
// a rebuild.
//
// Every stage — including release (the zero value or the "release" name) —
// promotes the digest to a single moving pointer <base>:<name>, where <base>
// is the registry/path of the entry's final tag (e.g. final
// "ghcr.io/o/r:v1.2.3" + stage "dev" → "ghcr.io/o/r:dev"; + "release" →
// "ghcr.io/o/r:release"). The immutable :<version> tag is applied once at
// build and is never re-written by promotion.
//
// Naming is deliberately registry-agnostic: the same <base>:<name> scheme
// works on ghcr, GitLab CR, and Codeberg/Forgejo alike, matching the
// ledger's existing cross-registry design.
type Stage struct {
	// Name is the stage label ("dev", "stage", "release"). Empty == release.
	Name string

	// TargetRepo optionally rehomes the promotion onto a different registry —
	// cross-registry / digital-sovereignty promotion (e.g. build on ghcr,
	// promote release to a Codeberg/Forgejo or Harbor registry you own). It is
	// a destination PREFIX — typically the registry host, optionally with a new
	// namespace — beneath which each image's source repository path is
	// preserved: <TargetRepo>/<source-path-after-host> (e.g. TargetRepo
	// "codeberg.org" + source "ghcr.io/org/app" → "codeberg.org/org/app"). This
	// maps every image to a distinct path by construction, so a multi-container
	// release never collides. A cross-registry release also carries the
	// immutable :<version> tag to the target. Empty keeps the entry's own
	// registry/path (the same-registry scheme).
	TargetRepo string

	// UseEntryReleaseTags switches release promotion from the generic
	// <base>:release pointer to the exact release destinations carried by the
	// ledger entry: final_tag and optional moving_tag. This is the build-once,
	// sign-before-publish model used by Forgejo releases where the immutable
	// final tag is not applied until after signing.
	UseEntryReleaseTags bool

	// AllowDigestRefFallback lets promotion recover when candidate_tag no longer
	// resolves to the recorded digest by copying from the entry's digest-pinned
	// ref instead. Off by default so existing users keep strict candidate-tag
	// validation unless they explicitly opt into Forgejo-style rerun recovery.
	AllowDigestRefFallback bool
}

// ReleaseStageName is the terminal stage; its destination is the <base>:release
// moving pointer on the same digest.
const ReleaseStageName = "release"

// IsRelease reports whether s is the terminal release stage — the zero
// value or the explicit "release" name.
func (s Stage) IsRelease() bool {
	return s.Name == "" || s.Name == ReleaseStageName
}

// Validate rejects a stage name that isn't safe as a tag component, so a
// typo can't mint a bogus <base>:<typo> tag. The release stage (which uses
// existing tags) always passes.
func (s Stage) Validate() error {
	if s.IsRelease() {
		return nil
	}

	if !stageNameRE.MatchString(s.Name) {
		return fmt.Errorf("imageledger: invalid stage name %q (must be a valid OCI tag component): %w", s.Name, errs.ErrUsage)
	}

	return nil
}

// destinations returns the ordered tags the recorded digest is promoted to
// for this stage, relative to the entry. Every stage — dev, staging, release —
// adds one moving pointer <base>:<stage> on the same digest; the immutable
// :<version> tag is applied once at build and never re-written here. base is
// TargetRepo when set, else the registry/path of the entry's final tag.
//
// One exception carries the full release across registries: a cross-registry
// RELEASE (TargetRepo set) also copies the immutable :<version> tag to the
// target, so a sovereign registry holds the complete release — the version tag
// and the :release pointer — not just a dangling pointer.
func (s Stage) destinations(entry Entry) []string {
	if s.IsRelease() && s.UseEntryReleaseTags {
		return s.entryReleaseDestinations(entry)
	}

	name := s.Name
	if name == "" {
		name = ReleaseStageName
	}

	base := container.StripTag(entry.FinalTag)
	if s.TargetRepo != "" {
		// Cross-registry: rehome under the target prefix while preserving the
		// source repository path (<prefix>/<source-path-after-host>), so every
		// image maps to a distinct destination by construction — no collision,
		// no dependency on a unique-name invariant.
		base = s.TargetRepo + "/" + repoPathAfterHost(entry.FinalTag)
	}

	dests := make([]string, 0, 2)
	if s.IsRelease() && s.TargetRepo != "" {
		dests = append(dests, base+":"+tagName(entry.FinalTag))
	}

	dests = append(dests, base+":"+name)

	return dests
}

func (s Stage) entryReleaseDestinations(entry Entry) []string {
	if s.TargetRepo == "" {
		dests := []string{entry.FinalTag}
		if entry.MovingTag != "" {
			dests = append(dests, entry.MovingTag)
		}

		return dests
	}

	base := s.TargetRepo + "/" + repoPathAfterHost(entry.FinalTag)

	dests := []string{base + ":" + tagName(entry.FinalTag)}
	if entry.MovingTag != "" {
		dests = append(dests, base+":"+tagName(entry.MovingTag))
	}

	return dests
}
