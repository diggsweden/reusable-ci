// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/version"
)

// releaseContextOps is the slice of adapter/git.Repo this use case needs.
type releaseContextOps interface {
	TaggerInfo(ctx context.Context, tag string) (git.TaggerInfo, error)
	TagsPointingAt(ctx context.Context, commit string) ([]string, error)
}

// ReleaseContextInput drives `reusable-ci version derive-release`.
type ReleaseContextInput struct {
	Ref                   string // the pushed ref, e.g. "release-request/v3.5.7"
	ResolveReleaseRequest bool   // resolve the sole release-request/v* tag at Revision
	Revision              string // defaults to HEAD only in ResolveReleaseRequest mode
	RequireReleaseRequest bool   // reject refs outside release-request/
	RequireStable         bool   // reject non-vMAJOR.MINOR.PATCH final tags
	TrailerMode           string // default or coauthor-only
}

// ReleaseContext derives the release identity from the pushed request ref
// and emits it as CI outputs, so workflows never hand-roll prefix-stripping
// or tagger extraction in bash. From `release-request/v3.5.7` it emits:
//
//	release-tag      = v3.5.7
//	version          = 3.5.7
//	release-request  = release-request/v3.5.7
//	commit-trailers  = (multi-line) git trailers recording the original tagger
//
// A non-request ref is treated as the release tag verbatim (release-request
// is then empty and no request trailer is added) so the command degrades
// cleanly. The commit-trailers block records WHO authorised the release —
// the original (human) tagger of the immutable, signed request tag — for the
// bot's release commit; the request tag itself remains the cryptographic
// anchor.
func ReleaseContext(ctx context.Context, repo releaseContextOps, in ReleaseContextInput, sink ci.OutputSink, manifest ci.ManifestSink, w io.Writer) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	ref, err := resolveReleaseContextRef(ctx, repo, in)
	if err != nil {
		return err
	}

	if ref == "" {
		return fmt.Errorf("release-context: ref is required: %w", errs.ErrUsage)
	}

	if strings.ContainsAny(ref, "\r\n") {
		return fmt.Errorf("release-context: ref must be a single-line value: %w", errs.ErrValidation)
	}

	trailerMode, err := normalizeTrailerMode(in.TrailerMode)
	if err != nil {
		return err
	}

	in.Ref = ref

	releaseTag, requestRef, err := deriveReleaseIdentity(in)
	if err != nil {
		return err
	}

	releaseVersion := version.StripVPrefix(releaseTag)

	trailers := buildReleaseTrailers(ctx, repo, requestRef, trailerMode)

	if err := emitReleaseContextOutputs(ctx, sink, manifest, releaseTag, releaseVersion, requestRef, trailers); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(w, "%s Release tag %s (version %s)\n", clicolor.Check(w), releaseTag, releaseVersion)

	return nil
}

// resolveReleaseContextRef keeps ordinary explicit-ref derivation unchanged.
// Only the opt-in mode consults git, and only that mode supplies HEAD as a
// default revision.
func resolveReleaseContextRef(ctx context.Context, repo releaseContextOps, in ReleaseContextInput) (string, error) {
	if !in.ResolveReleaseRequest {
		if in.Revision != "" {
			return "", fmt.Errorf("release-context: --revision requires --resolve-release-request: %w", errs.ErrUsage)
		}

		return in.Ref, nil
	}

	revision := cmp.Or(in.Revision, "HEAD")
	if strings.ContainsAny(revision, "\r\n") {
		return "", fmt.Errorf("release-context: revision must be a single-line value: %w", errs.ErrValidation)
	}

	tags, err := repo.TagsPointingAt(ctx, revision)
	if err != nil {
		return "", fmt.Errorf("release-context: list tags pointing at %q: %w", revision, err)
	}

	requests := make([]string, 0, len(tags))
	for _, tag := range tags {
		finalTag, ok := version.ReleaseRequestVersion(tag)
		if ok && strings.HasPrefix(finalTag, "v") {
			requests = append(requests, strings.TrimPrefix(tag, "refs/tags/"))
		}
	}

	sort.Strings(requests)

	if len(requests) != 1 {
		return "", fmt.Errorf("release-context: expected exactly one %sv* tag pointing at %q, found %d (%s): %w",
			version.ReleaseRequestPrefix, revision, len(requests), strings.Join(requests, ", "), errs.ErrValidation)
	}

	return requests[0], nil
}

// trailerModeDefault and trailerModeCoauthorOnly are the accepted
// ReleaseContextInput.TrailerMode values, named after the trailer shape they
// emit rather than any consumer: "default" adds Release-Authorized-By +
// Co-authored-by; "coauthor-only" emits Co-authored-by alone.
const (
	trailerModeDefault      = "default"
	trailerModeCoauthorOnly = "coauthor-only"
)

// normalizeTrailerMode applies the default trailer mode and rejects unknown
// modes.
func normalizeTrailerMode(mode string) (string, error) {
	switch mode {
	case "":
		return trailerModeDefault, nil
	case trailerModeDefault, trailerModeCoauthorOnly:
		return mode, nil
	default:
		return "", fmt.Errorf("release-context: trailer mode must be %q or %q (got %q): %w",
			trailerModeDefault, trailerModeCoauthorOnly, mode, errs.ErrUsage)
	}
}

// deriveReleaseIdentity resolves the release tag and (optional) request ref
// from the pushed ref, enforcing the require-request/require-stable policies.
func deriveReleaseIdentity(in ReleaseContextInput) (string, string, error) {
	releaseTag := in.Ref
	requestRef := ""

	if final, ok := version.ReleaseRequestVersion(in.Ref); ok {
		releaseTag = final
		requestRef = strings.TrimPrefix(in.Ref, "refs/tags/")
	} else if in.RequireReleaseRequest {
		return "", "", fmt.Errorf("release-context: ref must be %svMAJOR.MINOR.PATCH (got %q): %w", version.ReleaseRequestPrefix, in.Ref, errs.ErrValidation)
	}

	if in.RequireStable && !version.IsStableSemverTag(releaseTag) {
		return "", "", fmt.Errorf("release-context: release request must target stable vMAJOR.MINOR.PATCH (got %q): %w", in.Ref, errs.ErrValidation)
	}

	return releaseTag, requestRef, nil
}

// emitReleaseContextOutputs writes the derived identity as CI outputs; a
// nil sink (no CI output file) is a no-op.
func emitReleaseContextOutputs(ctx context.Context, sink ci.OutputSink, manifest ci.ManifestSink, releaseTag, releaseVersion, requestRef string, trailers []string) error {
	if sink == nil {
		return nil
	}

	// Fixed order (not a map range) so the output keys are emitted
	// deterministically — matching the rest of the codebase's byte-stable
	// output discipline.
	outputs := []struct{ key, value string }{
		{"release-tag", releaseTag},
		{"version", releaseVersion},
		{"release-request", requestRef},
	}
	for _, o := range outputs {
		if err := sink.Set(ctx, o.key, o.value); err != nil {
			return fmt.Errorf("emit %s: %w", o.key, err)
		}
	}

	if err := ci.EmitMultiline(ctx, sink, manifest, "release-context",
		ci.MultilineEntry{Key: "commit-trailers", Lines: trailers}); err != nil {
		return fmt.Errorf("emit commit-trailers: %w", err)
	}

	return nil
}

// buildReleaseTrailers assembles the git trailers that record the original
// tagger in the bot's release commit. For a request-driven release it adds a
// Release-Request pointer plus the tagger identity (Release-Authorized-By +
// Co-authored-by) read from the signed request tag. Best-effort: a missing
// or unreadable tagger just yields fewer trailers, never an error.
func buildReleaseTrailers(ctx context.Context, repo releaseContextOps, requestRef, mode string) []string {
	if requestRef == "" {
		return nil
	}

	trailers := []string{"Release-Request: " + requestRef}

	// An identity without both a name and an address names nobody, in
	// either mode: "Co-authored-by: Ada <>" is not attribution.
	info, err := repo.TaggerInfo(ctx, requestRef)
	if err != nil || !completeTaggerIdentity(info.Tagger) {
		return trailers
	}

	if mode == trailerModeCoauthorOnly {
		return append(trailers, "Co-authored-by: "+info.Tagger)
	}

	return append(trailers,
		"Release-Authorized-By: "+info.Tagger,
		"Co-authored-by: "+info.Tagger,
	)
}

func completeTaggerIdentity(identity string) bool {
	name, rest, ok := strings.Cut(identity, " <")
	if !ok || strings.TrimSpace(name) == "" {
		return false
	}

	email := strings.TrimSuffix(rest, ">")

	return strings.TrimSpace(email) != ""
}
