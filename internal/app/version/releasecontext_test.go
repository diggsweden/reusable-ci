// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type fakeReleaseContextRepo struct {
	tagger     git.TaggerInfo
	taggerErr  error
	tags       []string
	tagsErr    error
	tagQueries *[]string
}

func (f fakeReleaseContextRepo) TaggerInfo(_ context.Context, _ string) (git.TaggerInfo, error) {
	return f.tagger, f.taggerErr
}

func (f fakeReleaseContextRepo) TagsPointingAt(_ context.Context, revision string) ([]string, error) {
	if f.tagQueries != nil {
		*f.tagQueries = append(*f.tagQueries, revision)
	}

	return f.tags, f.tagsErr
}

func TestReleaseContext_DefaultModePreservesGenericOutputs(t *testing.T) {
	t.Parallel()

	sink := fakeoutputsink.New(t)

	var out bytes.Buffer

	err := appversion.ReleaseContext(context.Background(), fakeReleaseContextRepo{
		tagger: git.TaggerInfo{Tagger: "Ada Lovelace <ada@example.test>"},
	}, appversion.ReleaseContextInput{Ref: "release-request/v1.2.3"}, sink, nil, &out)
	if err != nil {
		t.Fatal(err)
	}

	if sink.Single("release-tag") != "v1.2.3" || sink.Single("version") != "1.2.3" || sink.Single("release-request") != "release-request/v1.2.3" {
		t.Fatalf("scalar outputs = %#v", sink.AllScalar())
	}

	wantTrailers := []string{
		"Release-Request: release-request/v1.2.3",
		"Release-Authorized-By: Ada Lovelace <ada@example.test>",
		"Co-authored-by: Ada Lovelace <ada@example.test>",
	}
	if got := sink.Multiline("commit-trailers"); !slices.Equal(got, wantTrailers) {
		t.Fatalf("commit-trailers = %#v, want %#v", got, wantTrailers)
	}

	if !strings.Contains(out.String(), "Release tag v1.2.3 (version 1.2.3)") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestReleaseContext_ForgejoStrictModeMatchesLegacyShellContract(t *testing.T) {
	t.Parallel()

	sink := fakeoutputsink.New(t)

	err := appversion.ReleaseContext(context.Background(), fakeReleaseContextRepo{
		tagger: git.TaggerInfo{Tagger: "Ada Lovelace <ada@example.test>"},
	}, appversion.ReleaseContextInput{
		Ref:                   "refs/tags/release-request/v1.2.3",
		RequireReleaseRequest: true,
		RequireStable:         true,
		TrailerMode:           "coauthor-only",
	}, sink, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}

	if sink.Single("release-tag") != "v1.2.3" || sink.Single("version") != "1.2.3" || sink.Single("release-request") != "release-request/v1.2.3" {
		t.Fatalf("scalar outputs = %#v", sink.AllScalar())
	}

	wantTrailers := []string{
		"Release-Request: release-request/v1.2.3",
		"Co-authored-by: Ada Lovelace <ada@example.test>",
	}
	if got := sink.Multiline("commit-trailers"); !slices.Equal(got, wantTrailers) {
		t.Fatalf("commit-trailers = %#v, want %#v", got, wantTrailers)
	}
}

func TestReleaseContext_ForgejoStrictModeSkipsIncompleteTagger(t *testing.T) {
	t.Parallel()

	sink := fakeoutputsink.New(t)

	err := appversion.ReleaseContext(context.Background(), fakeReleaseContextRepo{
		tagger: git.TaggerInfo{Tagger: "Ada Lovelace <>"},
	}, appversion.ReleaseContextInput{
		Ref:                   "release-request/v1.2.3",
		RequireReleaseRequest: true,
		RequireStable:         true,
		TrailerMode:           "coauthor-only",
	}, sink, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}

	if got := sink.Multiline("commit-trailers"); !slices.Equal(got, []string{"Release-Request: release-request/v1.2.3"}) {
		t.Fatalf("commit-trailers = %#v", got)
	}
}

func TestReleaseContext_StrictModeRejectsLegacyShellInvalidRefs(t *testing.T) {
	t.Parallel()

	for _, ref := range []string{"v1.2.3", "release-request/v1.2.3-rc1", "release-request/v1.2.3+build", "release-request/", "release-request/v1.2.3\n"} {
		t.Run(ref, func(t *testing.T) {
			err := appversion.ReleaseContext(context.Background(), fakeReleaseContextRepo{}, appversion.ReleaseContextInput{
				Ref:                   ref,
				RequireReleaseRequest: true,
				RequireStable:         true,
				TrailerMode:           "coauthor-only",
			}, fakeoutputsink.New(t), nil, &bytes.Buffer{})
			if !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("err = %v, want validation", err)
			}
		})
	}
}

func TestReleaseContext_RejectsUnknownTrailerMode(t *testing.T) {
	t.Parallel()

	err := appversion.ReleaseContext(context.Background(), fakeReleaseContextRepo{}, appversion.ReleaseContextInput{
		Ref:         "release-request/v1.2.3",
		TrailerMode: "unknown",
	}, fakeoutputsink.New(t), nil, &bytes.Buffer{})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want usage", err)
	}
}

func TestReleaseContext_ResolveReleaseRequestAtRevision(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		tags    []string
		wantTag string
		wantErr bool
	}{
		{name: "zero", tags: []string{"v1.2.3", "release-request/1.2.3", "release-request/stable"}, wantErr: true},
		{name: "one", tags: []string{"release-request/v1.2.3"}, wantTag: "v1.2.3"},
		{name: "multiple", tags: []string{"release-request/v1.2.3", "release-request/not-selected", "release-request/v1.2.4"}, wantErr: true},
		{name: "mixed", tags: []string{"v1.2.3", "release-request/1.2.3", "snapshot/test", "refs/tags/release-request/v1.2.3"}, wantTag: "v1.2.3"},
		{name: "v wildcard is not a semver validator", tags: []string{"release-request/versioned"}, wantTag: "versioned"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sink := fakeoutputsink.New(t)

			err := appversion.ReleaseContext(context.Background(), fakeReleaseContextRepo{tags: tc.tags}, appversion.ReleaseContextInput{
				ResolveReleaseRequest: true,
				Revision:              "abc123",
			}, sink, nil, &bytes.Buffer{})
			if tc.wantErr {
				if !errors.Is(err, errs.ErrValidation) {
					t.Fatalf("err = %v, want validation", err)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if got := sink.Single("release-tag"); got != tc.wantTag {
				t.Fatalf("release-tag = %q, want %q", got, tc.wantTag)
			}
		})
	}
}

func TestReleaseContext_ResolveModeDefaultsToHEADAndExplicitModeIsRequired(t *testing.T) {
	t.Parallel()

	var queries []string

	repo := fakeReleaseContextRepo{
		tags:       []string{"release-request/v2.0.0"},
		tagQueries: &queries,
	}

	err := appversion.ReleaseContext(context.Background(), repo, appversion.ReleaseContextInput{
		Ref:                   "ambient-branch-name",
		ResolveReleaseRequest: true,
	}, fakeoutputsink.New(t), nil, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(queries, []string{"HEAD"}) {
		t.Fatalf("tag queries = %v, want HEAD", queries)
	}

	queries = nil

	err = appversion.ReleaseContext(context.Background(), repo, appversion.ReleaseContextInput{
		Ref: "v2.0.0",
	}, fakeoutputsink.New(t), nil, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}

	if len(queries) != 0 {
		t.Fatalf("explicit ref mode unexpectedly queried tags: %v", queries)
	}
}

func TestReleaseContext_RevisionWithoutResolveModeIsRejected(t *testing.T) {
	t.Parallel()

	err := appversion.ReleaseContext(context.Background(), fakeReleaseContextRepo{}, appversion.ReleaseContextInput{
		Ref:      "v1.2.3",
		Revision: "HEAD",
	}, fakeoutputsink.New(t), nil, &bytes.Buffer{})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want usage", err)
	}
}

// recordingReleaseContextRepo records which tag the tagger was read from; the
// value fake above answers any tag, so reading the final tag instead of the
// signed request tag passed.
type recordingReleaseContextRepo struct {
	tagger  git.TaggerInfo
	queries []string
}

func (r *recordingReleaseContextRepo) TaggerInfo(_ context.Context, tag string) (git.TaggerInfo, error) {
	r.queries = append(r.queries, tag)

	return r.tagger, nil
}

func (r *recordingReleaseContextRepo) TagsPointingAt(context.Context, string) ([]string, error) {
	return nil, errors.New("not expected") //nolint:err113 // test double refusal.
}

// noMultilineReleaseSink is a recording sink without multi-line support, like
// GitLab's dotenv sink.
type noMultilineReleaseSink struct {
	*fakeoutputsink.Sink

	failKey string
}

func (s noMultilineReleaseSink) SetMultiline(context.Context, string, []string) error {
	return errs.ErrUnsupported
}

func (s noMultilineReleaseSink) Set(ctx context.Context, key, value string) error {
	if key == s.failKey {
		return errReleaseOutputRefused
	}

	return s.Sink.Set(ctx, key, value)
}

var errReleaseOutputRefused = errors.New("output refused")

// countingReleaseManifest counts stage manifest writes.
type countingReleaseManifest struct {
	stage  string
	result map[string]any
	writes int
}

func (m *countingReleaseManifest) Write(_ context.Context, stage string, result map[string]any) error {
	m.writes++
	m.stage, m.result = stage, result

	return nil
}

func (m *countingReleaseManifest) WriteJSON(context.Context, string, interface{ MarshalJSON() ([]byte, error) }) error {
	return errors.New("not expected") //nolint:err113 // test double refusal.
}

// TestReleaseContext_QueriesTheRequestTagAndDeliversInOrder pins the tagger
// query, the output order, the manifest fallback and failure behaviour. The
// tagger is read from the signed request tag, not the final tag. Scalars are
// written release-tag, version, release-request; on a sink without
// multi-line support the trailers go to exactly one release-context manifest
// write. A refused output stops the later ones and the success line, which is
// printed only once everything was delivered. An incomplete tagger yields no
// identity trailer in the default mode either.
func TestReleaseContext_QueriesTheRequestTagAndDeliversInOrder(t *testing.T) {
	t.Parallel()

	in := appversion.ReleaseContextInput{Ref: "refs/tags/release-request/v1.2.3"}

	t.Run("delivery", func(t *testing.T) {
		t.Parallel()

		repo := &recordingReleaseContextRepo{tagger: git.TaggerInfo{Tagger: "Ada Lovelace <ada@example.test>"}}
		sink := noMultilineReleaseSink{Sink: fakeoutputsink.New(t)}
		manifest := &countingReleaseManifest{}

		var out bytes.Buffer

		if err := appversion.ReleaseContext(context.Background(), repo, in, sink, manifest, &out); err != nil {
			t.Fatal(err)
		}

		if !slices.Equal(repo.queries, []string{"release-request/v1.2.3"}) {
			t.Errorf("tagger queries = %q, want the request tag", repo.queries)
		}

		if order := sink.Order(); !slices.Equal(order, []string{"release-tag", "version", "release-request"}) {
			t.Errorf("output order = %q", order)
		}

		want := []string{"Release-Request: release-request/v1.2.3", "Release-Authorized-By: Ada Lovelace <ada@example.test>", "Co-authored-by: Ada Lovelace <ada@example.test>"}
		if got, ok := manifest.result["commit-trailers"].([]string); manifest.writes != 1 || manifest.stage != "release-context" || len(manifest.result) != 1 || !ok || !slices.Equal(got, want) {
			t.Errorf("manifest writes = %d, stage = %q, result = %v", manifest.writes, manifest.stage, manifest.result)
		}

		if !strings.Contains(out.String(), "Release tag v1.2.3 (version 1.2.3)") {
			t.Errorf("stdout = %q", out.String())
		}
	})

	for _, key := range []string{"release-tag", "version", "release-request"} {
		t.Run("refused "+key, func(t *testing.T) {
			t.Parallel()

			sink := noMultilineReleaseSink{Sink: fakeoutputsink.New(t), failKey: key}
			manifest := &countingReleaseManifest{}

			var out bytes.Buffer

			err := appversion.ReleaseContext(context.Background(), &recordingReleaseContextRepo{}, in, sink, manifest, &out)
			if !errors.Is(err, errReleaseOutputRefused) || manifest.writes != 0 || out.Len() != 0 {
				t.Errorf("err = %v, manifest writes = %d, stdout = %q; want the refusal and nothing after it", err, manifest.writes, out.String())
			}

			for _, later := range sink.Order() {
				if later == key {
					t.Errorf("refused key %q was recorded", key)
				}
			}
		})
	}

	t.Run("incomplete tagger in the default mode", func(t *testing.T) {
		t.Parallel()

		sink := fakeoutputsink.New(t)

		err := appversion.ReleaseContext(context.Background(), &recordingReleaseContextRepo{tagger: git.TaggerInfo{Tagger: "Ada Lovelace <>"}}, in, sink, nil, io.Discard)
		if err != nil || !slices.Equal(sink.Multiline("commit-trailers"), []string{"Release-Request: release-request/v1.2.3"}) {
			t.Errorf("err = %v, trailers = %q; want only the request pointer", err, sink.Multiline("commit-trailers"))
		}
	})
}
