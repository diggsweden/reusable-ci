// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package baseimages_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/app/baseimages"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type fakeFreshnessResolver struct {
	digests map[string]string
	err     error
}

func (f *fakeFreshnessResolver) ResolveDigest(_ context.Context, ref string) (string, error) {
	if f.err != nil {
		return "", f.err
	}

	return f.digests[ref], nil
}

var errRegistryBlip = errors.New("registry blip")

const freshPinned = "docker.io/library/debian:trixie-slim@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func freshnessInput(enforce bool) baseimages.CheckFreshnessInput {
	return baseimages.CheckFreshnessInput{
		ChecksJSON: `[{"name":"DEBIAN_IMAGE","pinned-ref":"` + freshPinned + `","source-tag":"docker.io/library/debian:trixie-slim"}]`,
		Enforce:    enforce,
	}
}

func TestCheckFreshnessCurrent(t *testing.T) {
	t.Parallel()

	resolver := &fakeFreshnessResolver{digests: map[string]string{
		"docker.io/library/debian:trixie-slim": "sha256:" + strings.Repeat("a", 64),
	}}
	sink := fakeoutputsink.New(t)

	var out, stderr bytes.Buffer

	err := baseimages.CheckFreshness(context.Background(), resolver, sink, &out, &stderr, freshnessInput(true))
	if err != nil {
		t.Fatalf("CheckFreshness() = %v, want nil", err)
	}

	if got := sink.Single("stale-count"); got != "0" {
		t.Errorf("stale-count = %q, want 0", got)
	}

	if !strings.Contains(out.String(), "DEBIAN_IMAGE digest is current") {
		t.Errorf("out = %q, want current message", out.String())
	}
}

func TestCheckFreshnessStale(t *testing.T) {
	t.Parallel()

	resolver := &fakeFreshnessResolver{digests: map[string]string{
		"docker.io/library/debian:trixie-slim": "sha256:" + strings.Repeat("b", 64),
	}}

	t.Run("enforce fails", func(t *testing.T) {
		t.Parallel()

		var out, stderr bytes.Buffer

		err := baseimages.CheckFreshness(context.Background(), resolver, fakeoutputsink.New(t), &out, &stderr, freshnessInput(true))
		if !errors.Is(err, errs.ErrValidation) {
			t.Fatalf("CheckFreshness() = %v, want ErrValidation", err)
		}

		if !strings.Contains(stderr.String(), "ERROR: DEBIAN_IMAGE digest is stale") {
			t.Errorf("stderr = %q, want stale error", stderr.String())
		}
	})

	t.Run("warn-only passes with stale-count output", func(t *testing.T) {
		t.Parallel()

		sink := fakeoutputsink.New(t)

		var out, stderr bytes.Buffer

		err := baseimages.CheckFreshness(context.Background(), resolver, sink, &out, &stderr, freshnessInput(false))
		if err != nil {
			t.Fatalf("CheckFreshness() = %v, want nil", err)
		}

		if got := sink.Single("stale-count"); got != "1" {
			t.Errorf("stale-count = %q, want 1", got)
		}

		if !strings.Contains(stderr.String(), "not blocking") {
			t.Errorf("stderr = %q, want non-blocking warning", stderr.String())
		}
	})
}

func TestCheckFreshnessFetchFailure(t *testing.T) {
	t.Parallel()

	resolver := &fakeFreshnessResolver{err: errRegistryBlip}

	t.Run("enforce fails closed", func(t *testing.T) {
		t.Parallel()

		var out, stderr bytes.Buffer

		err := baseimages.CheckFreshness(context.Background(), resolver, fakeoutputsink.New(t), &out, &stderr, freshnessInput(true))
		if !errors.Is(err, errs.ErrDependencyUnavailable) {
			t.Fatalf("CheckFreshness() = %v, want ErrDependencyUnavailable", err)
		}
	})

	t.Run("warn-only skips", func(t *testing.T) {
		t.Parallel()

		var out, stderr bytes.Buffer

		err := baseimages.CheckFreshness(context.Background(), resolver, fakeoutputsink.New(t), &out, &stderr, freshnessInput(false))
		if err != nil {
			t.Fatalf("CheckFreshness() = %v, want nil", err)
		}

		if !strings.Contains(stderr.String(), "skipping (not blocking)") {
			t.Errorf("stderr = %q, want skip warning", stderr.String())
		}
	})
}

func TestCheckFreshnessConfigErrorsFailInBothModes(t *testing.T) {
	t.Parallel()

	resolver := &fakeFreshnessResolver{}

	cases := map[string]string{
		"unpinned ref":       `[{"name":"X","pinned-ref":"docker.io/library/debian:trixie-slim","source-tag":"docker.io/library/debian:trixie"}]`,
		"digest source tag":  `[{"name":"X","pinned-ref":"` + freshPinned + `","source-tag":"docker.io/library/debian@sha256:abc"}]`,
		"url source tag":     `[{"name":"X","pinned-ref":"` + freshPinned + `","source-tag":"https://docker.io/library/debian:trixie"}]`,
		"tagless source tag": `[{"name":"X","pinned-ref":"` + freshPinned + `","source-tag":"debian"}]`,
		"empty checks":       `[]`,
	}

	for name, checksJSON := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var out, stderr bytes.Buffer

			err := baseimages.CheckFreshness(context.Background(), resolver, fakeoutputsink.New(t), &out, &stderr,
				baseimages.CheckFreshnessInput{ChecksJSON: checksJSON, Enforce: false})
			if err == nil {
				t.Fatalf("CheckFreshness(%s) = nil, want config error even in warn-only mode", name)
			}
		})
	}
}
