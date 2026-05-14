// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package github_test

import (
	"context"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/adapters/github"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/internal/testutil/mockbinary"
)

// noExistingReleaseStub: gh release view exits non-zero (no release).
const noExistingReleaseStub = `
case "$1" in
  release)
    case "$2" in
      view)   echo "release not found" >&2; exit 1 ;;
      create) printf "https://github.com/owner/repo/releases/tag/v1.0.0\n" ;;
      delete) ;;
    esac
    ;;
esac
`

// existingDraftStub: gh release view returns isDraft=true.
const existingDraftStub = `
case "$1" in
  release)
    case "$2" in
      view)   printf '{"isDraft":true,"isPrerelease":false}'; exit 0 ;;
      delete) ;;
      create) ;;
    esac
    ;;
esac
`

// existingStableStub: gh release view returns a stable release.
const existingStableStub = `
case "$1" in
  release)
    case "$2" in
      view)   printf '{"isDraft":false,"isPrerelease":false}'; exit 0 ;;
      delete) ;;
      create) ;;
    esac
    ;;
esac
`

// existingPrereleaseStub: gh release view returns isPrerelease=true.
const existingPrereleaseStub = `
case "$1" in
  release)
    case "$2" in
      view)   printf '{"isDraft":false,"isPrerelease":true}'; exit 0 ;;
      delete) ;;
      create) ;;
    esac
    ;;
esac
`

func TestCreateRelease_NoExistingRelease(t *testing.T) {
	mock := mockbinary.New(t)
	mock.Add("gh", noExistingReleaseStub)
	p := &github.Provider{GHBin: mock.Path("gh")}
	err := p.CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
		Tag:        "v1.0.0",
		Name:       "v1.0.0",
		Draft:      false,
		MakeLatest: true,
		Assets:     []string{"foo.zip", "checksums.sha256"},
	})
	if err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}
	created := mock.Invocations("gh")
	// One `view`, then one `create` — no delete.
	wantSubcmds := []string{"view", "create"}
	for i, want := range wantSubcmds {
		if len(created) <= i || len(created[i].Args) < 2 {
			t.Fatalf("invocation %d missing args: %+v", i, created)
		}
		if created[i].Args[1] != want {
			t.Errorf("invocation %d subcmd = %q, want %q", i, created[i].Args[1], want)
		}
	}
	createArgs := created[1].Args
	hasFlag := func(flag string) bool {
		for _, a := range createArgs {
			if a == flag {
				return true
			}
		}
		return false
	}
	if !hasFlag("v1.0.0") || !hasFlag("--title") {
		t.Errorf("create args = %v", createArgs)
	}
	// `--draft` must be absent (Draft=false).
	if hasFlag("--draft") {
		t.Errorf("--draft should not appear when Draft=false: %v", createArgs)
	}
	// MakeLatest=true → no `--latest=false`.
	if hasFlag("--latest=false") {
		t.Errorf("--latest=false should not appear when MakeLatest=true: %v", createArgs)
	}
	// Assets present.
	if !hasFlag("foo.zip") || !hasFlag("checksums.sha256") {
		t.Errorf("missing assets in: %v", createArgs)
	}
}

func TestCreateRelease_DeletesExistingDraftOrPrerelease(t *testing.T) {
	tests := []struct {
		name string
		stub string
		tag  string
	}{
		{name: "draft", stub: existingDraftStub, tag: "v1.0.0"},
		{name: "prerelease", stub: existingPrereleaseStub, tag: "v1.0.0-rc.1"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			mock := mockbinary.New(t)
			mock.Add("gh", testCase.stub)
			p := &github.Provider{GHBin: mock.Path("gh")}
			err := p.CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
				Tag: testCase.tag, MakeLatest: true,
			})
			if err != nil {
				t.Fatalf("CreateRelease: %v", err)
			}
			calls := mock.Invocations("gh")
			if len(calls) < 3 {
				t.Fatalf("expected view+delete+create, got %d calls", len(calls))
			}
			subs := []string{calls[0].Args[1], calls[1].Args[1], calls[2].Args[1]}
			want := []string{"view", "delete", "create"}
			for i := range want {
				if subs[i] != want[i] {
					t.Errorf("subcmds = %v, want %v", subs, want)
				}
			}
		})
	}
}

func TestCreateRelease_RefusesExistingStable(t *testing.T) {
	mock := mockbinary.New(t)
	mock.Add("gh", existingStableStub)
	p := &github.Provider{GHBin: mock.Path("gh")}
	err := p.CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{Tag: "v1.0.0"})
	if err == nil || !strings.Contains(err.Error(), "Cannot overwrite") {
		t.Errorf("err = %v", err)
	}
}

func TestCreateRelease_DraftAndPrereleaseFlags(t *testing.T) {
	mock := mockbinary.New(t)
	mock.Add("gh", noExistingReleaseStub)
	p := &github.Provider{GHBin: mock.Path("gh")}
	err := p.CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
		Tag:        "v1.0.0-rc.1",
		Draft:      true,
		Prerelease: true,
		MakeLatest: false,
		NotesFile:  "release-notes.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := mock.Invocations("gh")
	createArgs := calls[1].Args
	want := map[string]bool{"--draft": false, "--prerelease": false, "--latest=false": false, "--notes-file": false}
	for _, a := range createArgs {
		if _, ok := want[a]; ok {
			want[a] = true
		}
	}
	for flag, found := range want {
		if !found {
			t.Errorf("missing flag %q in create args: %v", flag, createArgs)
		}
	}
}

func TestCreateRelease_EmptyTagErrors(t *testing.T) {
	t.Parallel()
	p := &github.Provider{}
	err := p.CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{})
	if err == nil || !strings.Contains(err.Error(), "tag is empty") {
		t.Errorf("err = %v", err)
	}
}
