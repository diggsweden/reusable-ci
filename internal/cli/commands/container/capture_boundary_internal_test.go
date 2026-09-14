// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
)

type captureRecorder struct {
	calls  []string
	ctx    context.Context //nolint:containedctx // records exact forwarding for the assertion; never reused to perform work.
	digest string
	err    error
}

func (r *captureRecorder) ResolveDigest(ctx context.Context, ref string) (string, error) {
	r.calls = append(r.calls, ref)
	r.ctx = ctx

	return r.digest, r.err
}

func TestCaptureBoundary_RefusalPreservesEntry(t *testing.T) {
	t.Parallel()

	cause := errors.New("owned resolver failure") //nolint:err113 // distinct dependency error.
	for _, tc := range []struct {
		source, digest    string
		resolverErr, want error
		calls             int
	}{
		{"", "", nil, errs.ErrUsage, 0},
		{"not a tag", "", nil, errs.ErrValidation, 0},
		{"registry.invalid/owner/image:v1", "SHA256:bad", nil, errs.ErrMalformedInput, 1},
		{"registry.invalid/owner/image:v1", "", cause, cause, 1},
		{"registry.invalid/owner/image:v1", "sha256:" + strings.Repeat("a", 64), nil, nil, 1},
	} {
		entry := imageledger.Entry{CandidateTag: tc.source, Ref: "seeded-ref", Digest: "seeded-digest", Role: "owned"}
		before := entry
		r := &captureRecorder{digest: tc.digest, err: tc.resolverErr}
		ctx := t.Context()

		err := captureEntryDigest(ctx, r, &entry)
		if !errors.Is(err, tc.want) || len(r.calls) != tc.calls {
			t.Fatalf("source=%q err=%v calls=%v", tc.source, err, r.calls)
		}

		if tc.calls == 1 && (r.calls[0] != tc.source || r.ctx != ctx) {
			t.Fatal("request identity not preserved")
		}

		if tc.want != nil {
			if !reflect.DeepEqual(entry, before) {
				t.Fatalf("refusal changed entry: %+v", entry)
			}
		} else if entry.Ref != "registry.invalid/owner/image@"+tc.digest || entry.Digest != tc.digest || entry.Role != "owned" {
			t.Fatalf("successful pin=%+v", entry)
		}
	}
}
