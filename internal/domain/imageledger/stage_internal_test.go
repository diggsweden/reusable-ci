// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger

import (
	"slices"
	"testing"
)

// TestStageDestinations locks in the build-once / promote-many tag scheme:
// every stage (dev, staging, release) promotes to a single <base>:<stage>
// moving pointer on the same digest; the immutable :<version> is build-only.
func TestStageDestinations(t *testing.T) {
	t.Parallel()

	base := "ghcr.io/owner/repo"
	entry := Entry{
		FinalTag:  base + ":v1.2.3",
		MovingTag: base + ":rust",
	}

	for _, tc := range []struct {
		name  string
		stage Stage
		entry Entry
		want  []stageDest
	}{
		{"release zero-value → :release pointer", Stage{}, entry, []stageDest{{Ref: base + ":release"}}},
		{"release explicit → :release pointer", Stage{Name: "release"}, entry, []stageDest{{Ref: base + ":release"}}},
		{"dev → <base>:dev pointer", Stage{Name: "dev"}, entry, []stageDest{{Ref: base + ":dev"}}},
		{"stage → <base>:stage pointer", Stage{Name: "stage"}, entry, []stageDest{{Ref: base + ":stage"}}},
		{
			"release with ledger tags → immutable final_tag + moving_tag",
			Stage{Name: "release", UseEntryReleaseTags: true},
			entry,
			[]stageDest{{Ref: base + ":v1.2.3", Immutable: true}, {Ref: base + ":rust"}},
		},
		{
			"cross-registry release with ledger tags preserves final and moving names",
			Stage{Name: "release", TargetRepo: "codeberg.org", UseEntryReleaseTags: true},
			entry,
			[]stageDest{{Ref: "codeberg.org/owner/repo:v1.2.3", Immutable: true}, {Ref: "codeberg.org/owner/repo:rust"}},
		},
		{
			"named stage with TargetRepo → <prefix>/<source-path> pointer",
			Stage{Name: "prod", TargetRepo: "codeberg.org/sovereign"},
			entry,
			[]stageDest{{Ref: "codeberg.org/sovereign/owner/repo:prod"}},
		},
		{
			"cross-registry release → immutable :<version> + :release, source path preserved",
			Stage{Name: "release", TargetRepo: "codeberg.org"},
			entry,
			[]stageDest{{Ref: "codeberg.org/owner/repo:v1.2.3", Immutable: true}, {Ref: "codeberg.org/owner/repo:release"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.stage.destinations(tc.entry); !slices.Equal(got, tc.want) {
				t.Errorf("destinations() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestStageDestinations_MultiContainerSovereigntyNoCollision locks in the fix
// for the monorepo case: two images promoted cross-registry under one target
// prefix must land on distinct <prefix>/<leaf> paths, never collapse onto one.
func TestStageDestinations_MultiContainerSovereigntyNoCollision(t *testing.T) {
	t.Parallel()

	stage := Stage{Name: "release", TargetRepo: "codeberg.org/sovereign"}
	apiEntry := Entry{FinalTag: "ghcr.io/org/api:v1.2.3"}
	webEntry := Entry{FinalTag: "ghcr.io/org/web:v1.2.3"}

	api := destRefs(stage.destinations(apiEntry))
	web := destRefs(stage.destinations(webEntry))

	wantAPI := []string{"codeberg.org/sovereign/org/api:v1.2.3", "codeberg.org/sovereign/org/api:release"}
	wantWeb := []string{"codeberg.org/sovereign/org/web:v1.2.3", "codeberg.org/sovereign/org/web:release"}

	if !slices.Equal(api, wantAPI) || !slices.Equal(web, wantWeb) {
		t.Fatalf("collision-free mapping broken:\n api = %v (want %v)\n web = %v (want %v)", api, wantAPI, web, wantWeb)
	}

	for _, a := range api {
		if slices.Contains(web, a) {
			t.Errorf("api and web share destination %q — images would overwrite each other", a)
		}
	}
}

func destRefs(dests []stageDest) []string {
	refs := make([]string, len(dests))
	for i, d := range dests {
		refs[i] = d.Ref
	}

	return refs
}

func TestStageIsRelease(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		want bool
	}{
		{"", true},
		{"release", true},
		{"dev", false},
		{"stage", false},
	} {
		if got := (Stage{Name: tc.name}).IsRelease(); got != tc.want {
			t.Errorf("Stage{%q}.IsRelease() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestStageValidate_RejectsEntryReleaseTagsOnNamedStage(t *testing.T) {
	t.Parallel()

	// UseEntryReleaseTags only has meaning on the release stage; on a named
	// stage it used to be silently ignored — now it is a caller error.
	if err := (Stage{Name: "dev", UseEntryReleaseTags: true}).Validate(); err == nil {
		t.Error("named stage with UseEntryReleaseTags should be rejected")
	}

	if err := (Stage{Name: "release", UseEntryReleaseTags: true}).Validate(); err != nil {
		t.Errorf("release stage with UseEntryReleaseTags rejected: %v", err)
	}
}
