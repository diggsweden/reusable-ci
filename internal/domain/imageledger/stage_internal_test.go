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
		FinalTag: base + ":v1.2.3",
	}

	for _, tc := range []struct {
		name  string
		stage Stage
		entry Entry
		want  []string
	}{
		{"release zero-value → :release pointer", Stage{}, entry, []string{base + ":release"}},
		{"release explicit → :release pointer", Stage{Name: "release"}, entry, []string{base + ":release"}},
		{"dev → <base>:dev pointer", Stage{Name: "dev"}, entry, []string{base + ":dev"}},
		{"stage → <base>:stage pointer", Stage{Name: "stage"}, entry, []string{base + ":stage"}},
		{
			"named stage with TargetRepo → <prefix>/<source-path> pointer",
			Stage{Name: "prod", TargetRepo: "codeberg.org/sovereign"},
			entry,
			[]string{"codeberg.org/sovereign/owner/repo:prod"},
		},
		{
			"cross-registry release → immutable :<version> + :release, source path preserved",
			Stage{Name: "release", TargetRepo: "codeberg.org"},
			entry,
			[]string{"codeberg.org/owner/repo:v1.2.3", "codeberg.org/owner/repo:release"},
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

	api := stage.destinations(apiEntry)
	web := stage.destinations(webEntry)

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
