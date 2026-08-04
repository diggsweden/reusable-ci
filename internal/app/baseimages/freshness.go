// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package baseimages

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// FreshnessCheck names one pinned base image and the moving registry tag
// its pin tracks.
type FreshnessCheck struct {
	// Name labels the check in output (e.g. the Containerfile ARG name).
	Name string `json:"name"`
	// PinnedRef is the digest-pinned ref actually built from
	// (registry/repo[:tag]@sha256:<hex>).
	PinnedRef string `json:"pinned-ref"`
	// SourceTag is the moving upstream tag the pin was taken from
	// (registry/repo:tag — never a URL or digest ref).
	SourceTag string `json:"source-tag"`
}

// CheckFreshnessInput drives CheckFreshness.
type CheckFreshnessInput struct {
	// ChecksJSON is a JSON array of FreshnessCheck objects.
	ChecksJSON string
	// Enforce selects the failure policy. True (the scheduled-cron mode):
	// a stale or unfetchable digest fails the run. False (the release
	// path): pins are for reproducibility, so drift and registry blips
	// only warn — currency is enforced out of band. Config errors (empty
	// or unpinned refs, malformed source tags) fail in either mode.
	Enforce bool
}

// CheckFreshness resolves each check's source tag in the registry and
// compares it with the pinned digest, reporting per-check status and a
// stale-count output. It is the forge-neutral core of the consumer-side
// base-image freshness gates.
func CheckFreshness(ctx context.Context, resolver imageDigestResolver, sink ci.OutputSink, out, stderr io.Writer, in CheckFreshnessInput) error {
	checks, err := parseFreshnessChecks(in.ChecksJSON)
	if err != nil {
		return err
	}

	stale := 0

	for _, check := range checks {
		isStale, err := checkOneFreshness(ctx, resolver, out, stderr, check, in.Enforce)
		if err != nil {
			return err
		}

		if isStale {
			stale++
		}
	}

	if err := sink.Set(ctx, "stale-count", strconv.Itoa(stale)); err != nil {
		return err
	}

	if stale == 0 {
		return nil
	}

	if in.Enforce {
		return fmt.Errorf("%d pinned container digest(s) are stale; bump the pins (renovate) and retry: %w", stale, errs.ErrValidation)
	}

	_, _ = fmt.Fprintf(stderr, "NOTE: %d pinned container digest(s) are behind upstream — not blocking this run.\n", stale)

	return nil
}

func parseFreshnessChecks(checksJSON string) ([]FreshnessCheck, error) {
	if strings.TrimSpace(checksJSON) == "" {
		return nil, fmt.Errorf("freshness: --checks-json is required: %w", errs.ErrUsage)
	}

	var checks []FreshnessCheck
	if err := json.Unmarshal([]byte(checksJSON), &checks); err != nil {
		return nil, fmt.Errorf("freshness: parse checks JSON: %w: %w", err, errs.ErrMalformedInput)
	}

	if len(checks) == 0 {
		return nil, fmt.Errorf("freshness: checks JSON is empty: %w", errs.ErrUsage)
	}

	for _, check := range checks {
		if err := validateFreshnessCheck(check); err != nil {
			return nil, err
		}
	}

	return checks, nil
}

// validateFreshnessCheck enforces the config contract that fails in BOTH
// modes: a named check, a digest-pinned built ref, and a plain moving
// registry tag to compare against.
func validateFreshnessCheck(check FreshnessCheck) error {
	if check.Name == "" {
		return fmt.Errorf("freshness: check name is empty: %w", errs.ErrUsage)
	}

	if check.PinnedRef == "" {
		return fmt.Errorf("freshness: %s pinned ref is empty: %w", check.Name, errs.ErrUsage)
	}

	if !strings.Contains(check.PinnedRef, "@sha256:") {
		return fmt.Errorf("freshness: %s image ref must be pinned by sha256 digest: %s: %w", check.Name, check.PinnedRef, errs.ErrValidation)
	}

	if check.SourceTag == "" {
		return fmt.Errorf("freshness: %s source tag is empty: %w", check.Name, errs.ErrUsage)
	}

	if strings.Contains(check.SourceTag, "://") || strings.Contains(check.SourceTag, "@") {
		return fmt.Errorf("freshness: %s source tag must be a registry tag, not URL or digest: %s: %w", check.Name, check.SourceTag, errs.ErrValidation)
	}

	slash := strings.Index(check.SourceTag, "/")
	if slash < 0 || !strings.Contains(check.SourceTag[slash:], ":") {
		return fmt.Errorf("freshness: %s source tag must include registry, repository, and tag: %s: %w", check.Name, check.SourceTag, errs.ErrValidation)
	}

	return nil
}

func checkOneFreshness(ctx context.Context, resolver imageDigestResolver, out, stderr io.Writer, check FreshnessCheck, enforce bool) (bool, error) {
	pinnedDigest := "sha256:" + check.PinnedRef[strings.LastIndex(check.PinnedRef, "@sha256:")+len("@sha256:"):]

	currentDigest, err := resolver.ResolveDigest(ctx, check.SourceTag)
	if err != nil || currentDigest == "" {
		if enforce {
			return false, fmt.Errorf("freshness: %s: fetch current digest for %s: %w: %w", check.Name, check.SourceTag, err, errs.ErrDependencyUnavailable)
		}

		_, _ = fmt.Fprintf(stderr, "WARNING: %s: could not fetch current digest for %s; skipping (not blocking)\n", check.Name, check.SourceTag)

		return false, nil
	}

	if pinnedDigest != currentDigest {
		level := "WARNING"
		note := " (not blocking; tracked by renovate + the freshness cron)"

		if enforce {
			level = "ERROR"
			note = ""
		}

		_, _ = fmt.Fprintf(stderr, "%s: %s digest is stale%s\n  pinned ref:     %s\n  source tag:     %s\n  pinned digest:  %s\n  current digest: %s\n",
			level, check.Name, note, check.PinnedRef, check.SourceTag, pinnedDigest, currentDigest)

		return true, nil
	}

	_, _ = fmt.Fprintf(out, "%s digest is current: %s\n", check.Name, pinnedDigest)

	return false, nil
}
