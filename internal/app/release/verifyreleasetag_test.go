// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"context"
	"errors"
	"io"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// fakeTagGit implements the tag-verify git surface.
type fakeTagGit struct {
	head      string
	tagCommit string
	tags      []string
}

func (f fakeTagGit) RevParse(_ context.Context, _ string) (string, error) { return f.head, nil }

func (f fakeTagGit) RemoteTagCommit(_ context.Context, _, _ string, _ runcontext.Credential) (string, error) {
	return f.tagCommit, nil
}

func (f fakeTagGit) RemoteVersionTags(_ context.Context, _ string, _ runcontext.Credential) ([]string, error) {
	return f.tags, nil
}

const sha = "abcdef1234567890abcdef1234567890abcdef12"

func runVerify(g fakeTagGit, tag string) error {
	return apprelease.VerifyReleaseTag(context.Background(), g, io.Discard, apprelease.VerifyReleaseTagInput{
		ReleaseSHA: sha, Tag: tag, RepoURL: "https://codeberg.org/o/r.git",
	})
}

func TestVerifyReleaseTag_Accepts(t *testing.T) {
	t.Parallel()

	g := fakeTagGit{head: sha, tagCommit: sha, tags: []string{"v1.0.0", "v1.2.3", "v1.1.0"}}
	if err := runVerify(g, "v1.2.3"); err != nil {
		t.Fatalf("valid release tag rejected: %v", err)
	}
}

func TestVerifyReleaseTag_Rejects(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		git fakeTagGit
		tag string
	}{
		"checkout_mismatch":   {fakeTagGit{head: "other", tagCommit: sha, tags: []string{"v1.2.3"}}, "v1.2.3"},
		"unstable_tag":        {fakeTagGit{head: sha, tagCommit: sha, tags: []string{"v1.2.3-rc1"}}, "v1.2.3-rc1"},
		"remote_tag_mismatch": {fakeTagGit{head: sha, tagCommit: "other", tags: []string{"v1.2.3"}}, "v1.2.3"},
		"superseded":          {fakeTagGit{head: sha, tagCommit: sha, tags: []string{"v1.2.3", "v2.0.0"}}, "v1.2.3"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := runVerify(tc.git, tc.tag); !errors.Is(err, errs.ErrValidation) {
				t.Errorf("expected validation error, got %v", err)
			}
		})
	}
}

func TestVerifyReleaseTag_MissingInputs(t *testing.T) {
	t.Parallel()

	err := apprelease.VerifyReleaseTag(context.Background(), fakeTagGit{}, io.Discard, apprelease.VerifyReleaseTagInput{})
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("missing inputs should be a usage error, got %v", err)
	}
}

func TestVerifyReleaseTag_IgnoresNonSemverTags(t *testing.T) {
	t.Parallel()

	// "latest", "v1.2.3-rc1", "nightly" must not count as a newer release.
	g := fakeTagGit{head: sha, tagCommit: sha, tags: []string{"v1.2.3", "latest", "v1.2.3-rc1", "nightly"}}
	if err := runVerify(g, "v1.2.3"); err != nil {
		t.Errorf("non-semver tags should be ignored, got %v", err)
	}
}
