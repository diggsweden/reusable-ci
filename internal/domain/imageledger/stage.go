// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger

import (
	"fmt"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// validStageName reports whether a stage name is safe to use as the tag
// component of a promoted ref (<base>:<name>). A stage name IS an OCI tag
// component, so it shares container's single-sourced charset rather than
// re-spelling it. A release stage (empty name) is exempt; it uses the entry's
// existing tags verbatim.
func validStageName(name string) bool { return container.ValidOCITagComponent(name) }

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
// works on ghcr, GitLab CR, and Forgejo alike, matching the
// ledger's existing cross-registry design.
type Stage struct {
	// Name is the stage label ("dev", "stage", "release"). Empty == release.
	Name string

	// TargetRepo optionally rehomes the promotion onto a different registry —
	// cross-registry / digital-sovereignty promotion (e.g. build on ghcr,
	// promote release to a Forgejo or Harbor registry you own). It is
	// a destination PREFIX — typically the registry host, optionally with a new
	// namespace — beneath which each image's source repository path is
	// preserved: <TargetRepo>/<source-path-after-host> (e.g. TargetRepo
	// "forgejo.example.com" + source "ghcr.io/org/app" → "forgejo.example.com/org/app"). This
	// maps every image to a distinct path by construction, so a multi-container
	// release never collides. A cross-registry release also carries the
	// immutable :<version> tag to the target. Empty keeps the entry's own
	// registry/path (the same-registry scheme).
	TargetRepo string

	// UseEntryReleaseTags switches release promotion from the generic
	// <base>:release pointer to the exact release destinations carried by the
	// ledger entry: final_tag and optional moving_tag.
	//
	// Use it whenever the immutable final tag is not applied until after
	// signing — the build-once, sign-before-publish model. That condition, not
	// a particular forge, is what selects this mode; it is named here because
	// Forgejo's release flow is where it first appeared, and it applies
	// unchanged on any registry.
	UseEntryReleaseTags bool

	// AllowDigestRefFallback lets promotion recover when candidate_tag no longer
	// resolves to the recorded digest by copying from the entry's digest-pinned
	// ref instead. Off by default, so strict candidate-tag validation stays the
	// rule unless a caller opts out of it.
	//
	// The condition it recovers from is a rerun whose candidate tag has since
	// been cleaned up or moved — forge-independent, and worth enabling on any
	// registry where release jobs are re-run. Note it cannot help on a registry
	// that drops untagged manifests, where the digest ref stops resolving once
	// no tag references it (see docs/providers.md).
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
// typo can't mint a bogus <base>:<typo> tag, and rejects flag combinations
// that would otherwise be silently ignored: UseEntryReleaseTags only has
// meaning on the release stage, so requesting it on a named stage is a
// caller error, not a no-op.
func (s Stage) Validate() error {
	if s.IsRelease() {
		return nil
	}

	if s.UseEntryReleaseTags {
		return fmt.Errorf("imageledger: stage %q cannot use ledger release tags (release stage only): %w", s.Name, errs.ErrUsage)
	}

	if !validStageName(s.Name) {
		return fmt.Errorf("imageledger: invalid stage name %q (must be a valid OCI tag component): %w", s.Name, errs.ErrUsage)
	}

	return nil
}

// stageDest is one promotion destination tag. Immutable marks a tag that
// must never move once present — the release final tag, or the cross-registry
// copy of the immutable :<version> tag. Promotion treats an existing
// immutable destination already serving the recorded digest as done
// (idempotent rerun) and refuses to overwrite one serving a different
// digest. Carrying this as a field, rather than by slice position, keeps
// promote and the rollback-journal planner from depending on ordering.
type stageDest struct {
	Ref       string
	Immutable bool
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
func (s Stage) destinations(entry Entry) []stageDest {
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

	dests := make([]stageDest, 0, 2)
	if s.IsRelease() && s.TargetRepo != "" {
		dests = append(dests, stageDest{Ref: base + ":" + tagName(entry.FinalTag), Immutable: true})
	}

	dests = append(dests, stageDest{Ref: base + ":" + name})

	return dests
}

func (s Stage) entryReleaseDestinations(entry Entry) []stageDest {
	if s.TargetRepo == "" {
		dests := []stageDest{{Ref: entry.FinalTag, Immutable: true}}
		if entry.MovingTag != "" {
			dests = append(dests, stageDest{Ref: entry.MovingTag})
		}

		return dests
	}

	base := s.TargetRepo + "/" + repoPathAfterHost(entry.FinalTag)

	dests := []stageDest{{Ref: base + ":" + tagName(entry.FinalTag), Immutable: true}}
	if entry.MovingTag != "" {
		dests = append(dests, stageDest{Ref: base + ":" + tagName(entry.MovingTag)})
	}

	return dests
}
