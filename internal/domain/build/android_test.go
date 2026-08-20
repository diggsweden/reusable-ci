// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/build"
)

func TestResolveAndroidArtifactNames_Override(t *testing.T) {
	got, err := build.ResolveAndroidArtifactNames(build.AndroidArtifactNamesInput{
		Override: "myapp-1.2.3",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := build.AndroidArtifactNames{
		DebugName:   "myapp-1.2.3-debug",
		ReleaseName: "myapp-1.2.3-release",
		AABName:     "myapp-1.2.3",
		SBOMName:    "myapp-1.2.3-sbom",
		AARName:     "myapp-1.2.3",
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestResolveAndroidArtifactNames_DateAndPrefixAndFlavor(t *testing.T) {
	got, err := build.ResolveAndroidArtifactNames(build.AndroidArtifactNamesInput{
		IncludeDate: true,
		Prefix:      "Nightly",
		RepoName:    "demo-app",
		Flavor:      "fdroid", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Today:       time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.DebugName != "2026-05-10 - Nightly - demo-app - fdroid - APK debug" {
		t.Errorf("debug = %q", got.DebugName)
	}

	if got.AABName != "2026-05-10 - Nightly - demo-app - fdroid - AAB release" {
		t.Errorf("aab = %q", got.AABName)
	}

	if got.SBOMName != "2026-05-10 - Nightly - demo-app - fdroid - build SBOM" {
		t.Errorf("sbom = %q", got.SBOMName)
	}
}

func TestResolveAndroidArtifactNames_NoDateNoPrefixNoFlavor(t *testing.T) {
	got, err := build.ResolveAndroidArtifactNames(build.AndroidArtifactNamesInput{
		RepoName: "demo-app",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.ReleaseName != "demo-app - APK release" {
		t.Errorf("release = %q", got.ReleaseName)
	}
}

func TestResolveAndroidArtifactNames_RequiresRepoOrOverride(t *testing.T) {
	_, err := build.ResolveAndroidArtifactNames(build.AndroidArtifactNamesInput{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveAndroidBuildTasks(t *testing.T) {
	cases := []struct {
		name string
		in   build.ResolveAndroidBuildTasksInput
		want string
	}{
		{
			name: "debug+release+aab/no-flavor",
			in:   build.ResolveAndroidBuildTasksInput{BuildTypes: "debug,release", IncludeAAB: true, BuildModule: "app"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			want: "assembleDebug assembleRelease app:bundleRelease",
		},
		{
			name: "release-only with flavor and AAB",
			in:   build.ResolveAndroidBuildTasksInput{Flavor: "fdroid", BuildTypes: "release", IncludeAAB: true, BuildModule: "app"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			want: "assembleFdroidRelease app:bundleFdroidRelease",
		},
		{
			name: "release-only no AAB",
			in:   build.ResolveAndroidBuildTasksInput{BuildTypes: "release", IncludeAAB: false, BuildModule: "app"},
			want: "assembleRelease",
		},
		{
			name: "debug-only doesn't include bundle even with AAB true",
			in:   build.ResolveAndroidBuildTasksInput{BuildTypes: "debug", IncludeAAB: true, BuildModule: "app"},
			want: "assembleDebug",
		},
		{
			// Library mode: an AAR, release-only, no bundle. IncludeAAB
			// and the debug build-type are deliberately ignored — a
			// library has no Play listing and no `bundle` task.
			name: "library mode derives the AAR task",
			in:   build.ResolveAndroidBuildTasksInput{BuildTypes: "debug,release", IncludeAAB: true, BuildModule: "lib", Library: true},
			want: "lib:assembleRelease",
		},
		{
			name: "library mode honours the flavor",
			in:   build.ResolveAndroidBuildTasksInput{Flavor: "demo", BuildTypes: "release", BuildModule: "lib", Library: true},
			want: "lib:assembleDemoRelease",
		},
		{
			name: "library mode defaults the module to app",
			in:   build.ResolveAndroidBuildTasksInput{BuildTypes: "release", Library: true},
			want: "app:assembleRelease",
		},
		{
			name: "default module is app",
			in:   build.ResolveAndroidBuildTasksInput{BuildTypes: "release", IncludeAAB: true},
			want: "assembleRelease app:bundleRelease",
		},
		{
			name: "mixed-case flavor capitalised",
			in:   build.ResolveAndroidBuildTasksInput{Flavor: "FDroid", BuildTypes: "release", BuildModule: "app"},
			want: "assembleFdroidRelease",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := build.ResolveAndroidBuildTasks(tc.in); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseGradleVersionFromProperties(t *testing.T) {
	body := "android.useAndroidX=true\nversionName=1.2.3\nversionCode=42\n"

	v, c := build.ParseGradleVersionFromProperties(body)
	if v != "1.2.3" || c != "42" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("got (%q, %q), want (\"1.2.3\", \"42\")", v, c)
	}
}

func TestParseGradleVersionFromProperties_DefaultsToUnknown(t *testing.T) {
	v, c := build.ParseGradleVersionFromProperties("# nothing useful\n")
	if v != "unknown" || c != "unknown" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("got (%q, %q), want (\"unknown\", \"unknown\")", v, c)
	}
}

func TestRenderAndroidSummary_FullVariant(t *testing.T) {
	now := time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC)

	got := build.RenderAndroidSummary(build.AndroidSummaryInput{
		JavaVersion: "25",
		JDKDist:     "Temurin", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		BuildModule: "app",
		Flavor:      "fdroid",
		BuildTypes:  "debug,release",
		IncludeAAB:  true,
		Signing:     true,
		SkipTests:   false,
		Version:     "1.2.3",
		VersionCode: "42",
		DebugName:   "demo-debug",
		ReleaseName: "demo-release",
		AABName:     "demo-aab",
	}, now)
	for _, want := range []string{
		"## Android Variants Build Summary 📱",
		"| **Java** | 25 (Temurin) |",
		"| **Module** | app |",
		"| **Flavor** | fdroid |",
		"| **Build Types** | debug,release |",
		"| **Include AAB** | ✓ |",
		"| **Signing** | ✓ Enabled |",
		"| **Tests** | ✓ Executed |",
		"| **Version** | 1.2.3 (42) |",
		"### Artifacts Generated",
		"✓ Debug APK: `demo-debug`",
		"✓ Release APK: `demo-release`",
		"✓ Release AAB: `demo-aab`",
		"*Build completed at 2026-05-10 14:00:00 UTC*",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderAndroidSummary_OmitsVersionWhenUnknown(t *testing.T) {
	now := time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC)

	got := build.RenderAndroidSummary(build.AndroidSummaryInput{
		JavaVersion: "25", JDKDist: "Temurin", BuildModule: "app",
		BuildTypes: "debug", Version: "unknown",
	}, now)
	if strings.Contains(got, "**Version**") {
		t.Errorf("expected no version row:\n%s", got)
	}

	if strings.Contains(got, "Release APK") {
		t.Errorf("did not expect release APK row in debug-only build:\n%s", got)
	}
}

func TestRenderAndroidSummary_FlavorDefaultsToDefault(t *testing.T) {
	got := build.RenderAndroidSummary(build.AndroidSummaryInput{
		JavaVersion: "25", JDKDist: "Temurin", BuildModule: "app",
		BuildTypes: "release", IncludeAAB: false,
	}, time.Now())
	if !strings.Contains(got, "| **Flavor** | default |") {
		t.Errorf("missing default flavor row:\n%s", got)
	}
}

// Library mode must never derive a `bundle` task: bundleRelease does not
// exist on an Android library module, so emitting it is a hard gradle
// failure rather than an extra artefact.
func TestResolveAndroidBuildTasks_LibraryNeverBundles(t *testing.T) {
	for _, in := range []build.ResolveAndroidBuildTasksInput{
		{BuildTypes: "debug,release", IncludeAAB: true, BuildModule: "lib", Library: true},
		{BuildTypes: "release", IncludeAAB: true, Flavor: "prod", Library: true},
		{BuildTypes: "debug", IncludeAAB: true, BuildModule: "sdk", Library: true},
	} {
		if got := build.ResolveAndroidBuildTasks(in); strings.Contains(got, "bundle") {
			t.Errorf("library mode derived a bundle task: %q", got)
		}
	}
}

// The app path must be byte-for-byte unchanged by the library addition —
// this is the regression guard for existing Android app adopters.
func TestResolveAndroidBuildTasks_AppPathUnaffectedByLibraryField(t *testing.T) {
	for _, in := range []build.ResolveAndroidBuildTasksInput{
		{BuildTypes: "debug,release", IncludeAAB: true, BuildModule: "app"},
		{Flavor: "demo", BuildTypes: "release", IncludeAAB: true, BuildModule: "app"},
		{BuildTypes: "release", IncludeAAB: false},
		{BuildTypes: "debug", IncludeAAB: true, BuildModule: "app"},
	} {
		withFalse := in
		withFalse.Library = false

		if got, want := build.ResolveAndroidBuildTasks(withFalse), build.ResolveAndroidBuildTasks(in); got != want {
			t.Errorf("Library:false changed the app derivation: %q vs %q", got, want)
		}
	}
}

// The AAR is the library's primary artifact, so its upload name must be
// the bare override — the same unsuffixed form the AAB uses for apps.
//
// This matters because the orchestrator passes artifacts.yml `name:` as
// the override, and downstream download steps resolve the artifact by
// PlannedArtifact.BuildArtifactName, which is that exact string. Using
// the "-release" suffixed ReleaseName would upload the AAR under a name
// nothing looks for.
func TestResolveAndroidArtifactNames_AARMatchesPrimaryName(t *testing.T) {
	got, err := build.ResolveAndroidArtifactNames(build.AndroidArtifactNamesInput{Override: "my-android-lib"})
	if err != nil {
		t.Fatal(err)
	}

	if got.AARName != "my-android-lib" {
		t.Errorf("AARName = %q, want the bare override", got.AARName)
	}

	if got.AARName != got.AABName {
		t.Errorf("AARName %q and AABName %q should share the primary-artifact name", got.AARName, got.AABName)
	}

	if got.AARName == got.ReleaseName {
		t.Errorf("AARName must not be the suffixed ReleaseName (%q)", got.ReleaseName)
	}
}

func TestResolveAndroidArtifactNames_AARDerivedForm(t *testing.T) {
	got, err := build.ResolveAndroidArtifactNames(build.AndroidArtifactNamesInput{RepoName: "my-lib"})
	if err != nil {
		t.Fatal(err)
	}

	if got.AARName != "my-lib - AAR release" {
		t.Errorf("AARName = %q", got.AARName)
	}
}
