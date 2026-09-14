// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestRenderAndroidSummary_RendersMetadataLiterally(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, raw, text string }{
		{"heading injection", "demo\n\n## B8-INJECTED\n", "demo  &#35;&#35; B8-INJECTED "},
		{"inline syntax", "[link](https://evil.invalid) <b>&amp;</b> \\| `` *_~\t\r\n", "&#91;link&#93;(https&#58;//evil.invalid) &#60;b&#62;&#38;amp;&#60;/b&#62; &#92;&#124; &#96;&#96; &#42;&#95;&#126;   "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			in := build.AndroidSummaryInput{
				JavaVersion: "java-" + tc.raw, JDKDist: "jdk-" + tc.raw, BuildModule: "module-" + tc.raw,
				Flavor: "flavor-" + tc.raw, BuildTypes: "debug,\trelease\r\n", IncludeAAB: true,
				Version: "version-" + tc.raw, VersionCode: "code-" + tc.raw,
				DebugName: "debug-" + tc.raw, ReleaseName: "release-" + tc.raw, AABName: "aab-" + tc.raw,
			}
			got := build.RenderAndroidSummary(in, time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC))

			want := fmt.Sprintf("## Android Variants Build Summary 📱\n\n### Configuration\n| Setting | Value |\n|---------|-------|\n| **Java** | java-%s (jdk-%s) |\n| **Module** | module-%s |\n| **Flavor** | flavor-%s |\n| **Build Types** | debug, release   |\n| **Include AAB** | ✓ |\n| **Signing** | ⊘ Disabled |\n| **Tests** | ✓ Executed |\n| **Version** | version-%s (code-%s) |\n\n### Artifacts Generated\n✓ Debug APK: <code>debug-%s</code>\n✓ Release APK: <code>release-%s</code>\n✓ Release AAB: <code>aab-%s</code>\n\n*Build completed at 2026-05-10 14:00:00 UTC*\n", tc.text, tc.text, tc.text, tc.text, tc.text, tc.text, tc.text, tc.text, tc.text)
			if got != want {
				t.Errorf("summary = %q, want %q", got, want)
			}
		})
	}
}

func TestRenderAndroidSummary_RawSelectionsAndStatusOutcomes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, types, typesText, version, versionRow string
		aab, signing, skipped                       bool
		aabStatus, signingStatus, testStatus        string
		artifacts                                   string
	}{
		{"release without AAB", "release", "release", "", "", false, false, true, "✗", "⊘ Disabled", "⊘ Skipped", "✓ Release APK: `release.apk`\n"},
		{"debug excludes release and AAB", "debug", "debug", "unknown", "", true, true, false, "✓", "✓ Enabled", "✓ Executed", "✓ Debug APK: `debug.apk`\n"},
		{"freeform types stay data", "demo\n\n## B8-INJECTED\n", "demo  &#35;&#35; B8-INJECTED ", " ", "| **Version** |   (42) |\n", true, false, true, "✓", "⊘ Disabled", "⊘ Skipped", ""},
		{"controls cannot enable variants", "debug\x00release", "debug release", "\nunknown", "| **Version** |  unknown (42) |\n", true, false, false, "✓", "⊘ Disabled", "✓ Executed", ""},
		{"syntax cannot enable variants", "debug|`[x]<b>&\\*_~", "debug&#124;&#96;&#91;x&#93;&#60;b&#62;&#38;&#92;&#42;&#95;&#126;", "", "", false, true, true, "✗", "✓ Enabled", "⊘ Skipped", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := build.RenderAndroidSummary(build.AndroidSummaryInput{
				JavaVersion: "25", JDKDist: "Temurin", BuildModule: "app", BuildTypes: tc.types,
				IncludeAAB: tc.aab, Signing: tc.signing, SkipTests: tc.skipped, Version: tc.version, VersionCode: "42",
				DebugName: "debug.apk", ReleaseName: "release.apk", AABName: "bundle.aab",
			}, time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC))

			want := fmt.Sprintf("## Android Variants Build Summary 📱\n\n### Configuration\n| Setting | Value |\n|---------|-------|\n| **Java** | 25 (Temurin) |\n| **Module** | app |\n| **Flavor** | default |\n| **Build Types** | %s |\n| **Include AAB** | %s |\n| **Signing** | %s |\n| **Tests** | %s |\n%s\n### Artifacts Generated\n%s\n*Build completed at 2026-05-10 14:00:00 UTC*\n", tc.typesText, tc.aabStatus, tc.signingStatus, tc.testStatus, tc.versionRow, tc.artifacts)
			if got != want {
				t.Errorf("summary = %q, want %q", got, want)
			}
		})
	}
}

func TestResolveAndroidArtifactNames_Override(t *testing.T) {
	t.Parallel()

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
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestResolveAndroidArtifactNames_DateAndPrefixAndFlavor(t *testing.T) {
	t.Parallel()

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

	if got.ReleaseName != "2026-05-10 - Nightly - demo-app - fdroid - APK release" {
		t.Errorf("release = %q", got.ReleaseName)
	}

	if got.AABName != "2026-05-10 - Nightly - demo-app - fdroid - AAB release" {
		t.Errorf("aab = %q", got.AABName)
	}

	if got.SBOMName != "2026-05-10 - Nightly - demo-app - fdroid - build SBOM" {
		t.Errorf("sbom = %q", got.SBOMName)
	}
}

func TestResolveAndroidArtifactNames_NoDateNoPrefixNoFlavor(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	_, err := build.ResolveAndroidArtifactNames(build.AndroidArtifactNamesInput{})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}
}

func TestResolveAndroidBuildTasks_BuildsTaskListFromTypesAndFlavor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		in      build.ResolveAndroidBuildTasksInput
		want    string
		wantErr bool
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
			name: "default module is app",
			in:   build.ResolveAndroidBuildTasksInput{BuildTypes: "release", IncludeAAB: true},
			want: "assembleRelease app:bundleRelease",
		},
		{
			name: "mixed-case flavor preserves its remainder",
			in:   build.ResolveAndroidBuildTasksInput{Flavor: "FDroid", BuildTypes: "release", BuildModule: "app"},
			want: "assembleFDroidRelease",
		},
		{
			name: "a camelCase flavor remains camelCase",
			in:   build.ResolveAndroidBuildTasksInput{Flavor: "proDemo", BuildTypes: "release", BuildModule: "app"},
			want: "assembleProDemoRelease",
		},
		{
			// "app" everywhere above is also the default, so none of those
			// cases could tell a module that is used from one that is ignored.
			name: "an explicit module names the bundle task",
			in:   build.ResolveAndroidBuildTasksInput{Flavor: "fdroid", BuildTypes: "release", IncludeAAB: true, BuildModule: "client"},
			want: "assembleFdroidRelease client:bundleFdroidRelease",
		},
		{
			name: "a whitespace module falls back to the default",
			in:   build.ResolveAndroidBuildTasksInput{BuildTypes: "release", IncludeAAB: true, BuildModule: "   "},
			want: "assembleRelease app:bundleRelease",
		},
		{
			name: "a padded module is trimmed",
			in:   build.ResolveAndroidBuildTasksInput{BuildTypes: "release", IncludeAAB: true, BuildModule: " client "},
			want: "assembleRelease client:bundleRelease",
		},
		{
			name:    "a module with inner whitespace would split into two tasks",
			in:      build.ResolveAndroidBuildTasksInput{BuildTypes: "release", IncludeAAB: true, BuildModule: "app extra"},
			wantErr: true,
		},
		{
			name: "mixed separators and a duplicate token",
			in:   build.ResolveAndroidBuildTasksInput{BuildTypes: " release ,debug\trelease", BuildModule: "app"},
			want: "assembleDebug assembleRelease",
		},
		{
			name:    "no build types at all",
			in:      build.ResolveAndroidBuildTasksInput{BuildTypes: " , \n", BuildModule: "app"},
			wantErr: true,
		},
		{
			name:    "one valid and one invalid token",
			in:      build.ResolveAndroidBuildTasksInput{BuildTypes: "release,staging", BuildModule: "app"},
			wantErr: true,
		},
		{
			name:    "a misspelled build type is rejected",
			in:      build.ResolveAndroidBuildTasksInput{BuildTypes: "relase", IncludeAAB: true, BuildModule: "app"},
			wantErr: true,
		},
		{
			name:    "substring is not a build type",
			in:      build.ResolveAndroidBuildTasksInput{BuildTypes: "notdebug", BuildModule: "app"},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := build.ResolveAndroidBuildTasks(tc.in)
			if tc.wantErr {
				if !errors.Is(err, errs.ErrValidation) {
					t.Fatalf("error = %v, want ErrValidation", err)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestParseGradleVersionFromProperties_ReadsVersionNameAndCode covers the
// whitespace and first-assignment behavior shared with the Gradle reader.
func TestParseGradleVersionFromProperties_ReadsVersionNameAndCode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		body        string
		wantVersion string
		wantCode    string
	}{
		{
			name:        "plain assignments among other properties",
			body:        "android.useAndroidX=true\nversionName=1.2.3\nversionCode=42\n",
			wantVersion: "1.2.3",
			wantCode:    "42",
		},
		{name: "space before separator", body: "versionName = 1.2.3\nversionCode = 42\n", wantVersion: "1.2.3", wantCode: "42"},
		{name: "tabs around separator", body: "\tversionName\t=\t1.2.3\t\r\nversionCode \t=\t42 \r\n", wantVersion: "1.2.3", wantCode: "42"},
		{name: "ignore comments and extended names", body: "#versionName = 9\n!versionCode = 90\nxversionName = 8\nversionCodeExtra = 80\nversionName = 1.2.3\nversionCode = 42\n", wantVersion: "1.2.3", wantCode: "42"},
		{name: "first spaced assignments win", body: "versionName = 1.2.3\nversionName = 9\nversionCode = 42\nversionCode = 99\n", wantVersion: "1.2.3", wantCode: "42"},
		// The duplicates of versionCode come BEFORE versionName here. In the
		// case above the loop stops once both keys are set, so the second
		// versionCode is never read and a missing first-wins guard on that key
		// could not show.
		{name: "versionCode duplicates before versionName", body: "versionCode=42\nversionCode=99\nversionName=1.2.3\n", wantVersion: "1.2.3", wantCode: "42"},
		{name: "versionName duplicates before versionCode", body: "versionName=1.2.3\nversionName=9\nversionCode=42\n", wantVersion: "1.2.3", wantCode: "42"},
		{name: "only the code present", body: "versionCode=42\n", wantVersion: "unknown", wantCode: "42"},
		{name: "unsupported separators stay unknown", body: "versionName: 1.2.3\nversionCode 42\n", wantVersion: "unknown", wantCode: "unknown"},
		{
			name:        "nothing useful falls back to unknown",
			body:        "# nothing useful\n",
			wantVersion: "unknown",
			wantCode:    "unknown",
		},
		{
			name:        "only one of the two present",
			body:        "versionName=1.2.3\n",
			wantVersion: "1.2.3",
			wantCode:    "unknown",
		},
		{
			name:        "CRLF is normalized",
			body:        "versionName=1.2.3\r\nversionCode=42\r\n",
			wantVersion: "1.2.3",
			wantCode:    "42",
		},
		{
			name:        "an indented assignment is normalized",
			body:        "  versionName=1.2.3\n  versionCode=42\n",
			wantVersion: "1.2.3",
			wantCode:    "42",
		},
		{
			name:        "surrounding spaces are normalized",
			body:        "versionName=1.2.3  \nversionCode= 42 \n",
			wantVersion: "1.2.3",
			wantCode:    "42",
		},
		{
			name:        "the first assignment wins",
			body:        "versionName=1.0\nversionName=2.0\n",
			wantVersion: "1.0",
			wantCode:    "unknown",
		},
		{
			name:        "a commented assignment is not read",
			body:        "#versionName=9.9.9\nversionName=1.2.3\n",
			wantVersion: "1.2.3",
			wantCode:    "unknown",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			v, c := build.ParseGradleVersionFromProperties(tc.body) //nolint:varnamelen // mirrors the function's own two-value shape.
			if v != tc.wantVersion || c != tc.wantCode {
				t.Errorf("got (%q, %q), want (%q, %q)", v, c, tc.wantVersion, tc.wantCode)
			}
		})
	}
}

func TestRenderAndroidSummary_FullVariant(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

	got := build.RenderAndroidSummary(build.AndroidSummaryInput{
		JavaVersion: "25", JDKDist: "Temurin", BuildModule: "app",
		BuildTypes: "release", IncludeAAB: false,
	}, time.Now())
	if !strings.Contains(got, "| **Flavor** | default |") {
		t.Errorf("missing default flavor row:\n%s", got)
	}
}

func TestRenderAndroidSummary_DoesNotSubstringMatchBuildTypes(t *testing.T) {
	t.Parallel()

	got := build.RenderAndroidSummary(build.AndroidSummaryInput{
		BuildTypes: "notdebug,prerelease", DebugName: "debug.apk", ReleaseName: "release.apk", IncludeAAB: true,
	}, time.Now())
	for _, unexpected := range []string{"Debug APK", "Release APK", "Release AAB"} {
		if strings.Contains(got, unexpected) {
			t.Errorf("summary substring-matched %q:\n%s", unexpected, got)
		}
	}
}
