// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version

import (
	"context"
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/internal/clicolor"
	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/version"
)

// releaseContextOps is the slice of adapter/git.Repo this use case needs.
type releaseContextOps interface {
	TaggerInfo(ctx context.Context, tag string) (string, string, error)
}

// ReleaseContextInput drives `reusable-ci version release-context`.
type ReleaseContextInput struct {
	Ref string // the pushed ref, e.g. "release-request/v3.5.7"
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
func ReleaseContext(ctx context.Context, repo releaseContextOps, in ReleaseContextInput, sink ci.OutputSink, w io.Writer) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if in.Ref == "" {
		return fmt.Errorf("release-context: ref is required: %w", errs.ErrUsage)
	}

	releaseTag := in.Ref
	requestRef := ""

	if final, ok := version.ReleaseRequestVersion(in.Ref); ok {
		releaseTag = final
		requestRef = in.Ref
	}

	releaseVersion := version.StripVPrefix(releaseTag)

	_, _ = fmt.Fprintf(w, "%s Release tag %s (version %s)\n", clicolor.Check(w), releaseTag, releaseVersion)

	trailers := buildReleaseTrailers(ctx, repo, requestRef)

	if sink == nil {
		return nil
	}

	for key, value := range map[string]string{
		"release-tag":     releaseTag,
		"version":         releaseVersion,
		"release-request": requestRef,
	} {
		if err := sink.Set(ctx, key, value); err != nil {
			return fmt.Errorf("emit %s: %w", key, err)
		}
	}

	if err := sink.SetMultiline(ctx, "commit-trailers", trailers); err != nil {
		return fmt.Errorf("emit commit-trailers: %w", err)
	}

	return nil
}

// buildReleaseTrailers assembles the git trailers that record the original
// tagger in the bot's release commit. For a request-driven release it adds a
// Release-Request pointer plus the tagger identity (Release-Authorized-By +
// Co-authored-by) read from the signed request tag. Best-effort: a missing
// or unreadable tagger just yields fewer trailers, never an error.
func buildReleaseTrailers(ctx context.Context, repo releaseContextOps, requestRef string) []string {
	if requestRef == "" {
		return nil
	}

	trailers := []string{"Release-Request: " + requestRef}

	if tagger, _, err := repo.TaggerInfo(ctx, requestRef); err == nil && tagger != "" {
		trailers = append(trailers,
			"Release-Authorized-By: "+tagger,
			"Co-authored-by: "+tagger,
		)
	}

	return trailers
}
