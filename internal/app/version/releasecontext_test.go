// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type fakeReleaseContextRepo struct {
	tagger git.TaggerInfo
	err    error
}

func (f fakeReleaseContextRepo) TaggerInfo(_ context.Context, _ string) (git.TaggerInfo, error) {
	return f.tagger, f.err
}

func TestReleaseContext_DefaultModePreservesGenericOutputs(t *testing.T) {
	t.Parallel()

	sink := fakeoutputsink.New(t)

	var out bytes.Buffer

	err := appversion.ReleaseContext(context.Background(), fakeReleaseContextRepo{
		tagger: git.TaggerInfo{Tagger: "Ada Lovelace <ada@example.test>"},
	}, appversion.ReleaseContextInput{Ref: "release-request/v1.2.3"}, sink, &out)
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
		TrailerMode:           "forgejo-ci",
	}, sink, &bytes.Buffer{})
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
		TrailerMode:           "forgejo-ci",
	}, sink, &bytes.Buffer{})
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
				TrailerMode:           "forgejo-ci",
			}, fakeoutputsink.New(t), &bytes.Buffer{})
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
	}, fakeoutputsink.New(t), &bytes.Buffer{})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want usage", err)
	}
}
