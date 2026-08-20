// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"strings"
	"testing"
	"time"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
)

func TestAndroidBuild_FullVariant(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.AndroidBuild(context.Background(), sink, appsummary.AndroidBuildInput{
		JavaVersion: "25",
		JDKDist:     "Temurin",
		BuildModule: "app", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Flavor:      "fdroid",
		BuildTypes:  "debug,release",
		IncludeAAB:  true,
		Signing:     true,
		SkipTests:   false,
		Version:     "1.2.3", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		VersionCode: "42",
		DebugName:   "demo-debug",
		ReleaseName: "demo-release",
		AABName:     "demo-aab",
		Now:         time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	got := sink.buf.String()
	for _, want := range []string{
		"## Android Variants Build Summary 📱",
		"| **Java** | 25 (Temurin) |",
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

func TestAndroidBuild_DefaultFlavorAndSigningDisabled(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.AndroidBuild(context.Background(), sink, appsummary.AndroidBuildInput{
		JavaVersion: "17",
		JDKDist:     "temurin",
		BuildModule: "app",
		Flavor:      "",
		BuildTypes:  "debug,release",
		IncludeAAB:  true,
		Signing:     false,
		SkipTests:   true,
		Version:     "1.0.0",
		VersionCode: "42",
		DebugName:   "debug-name",
		ReleaseName: "release-name",
		AABName:     "aab-name",
		Now:         time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	body := sink.buf.String()
	for _, want := range []string{"default", "Disabled", "⊘ Skipped"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
}

// A library build receives the application path's BuildTypes/IncludeAAB
// defaults from the workflow — the summary must ignore them and report
// the one artefact the build actually produced.
func TestAndroidBuild_LibraryReportsAARNotAPKOrAAB(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.AndroidBuild(context.Background(), sink, appsummary.AndroidBuildInput{
		JavaVersion: "25",
		JDKDist:     "Temurin",
		BuildModule: "lib",
		BuildTypes:  "debug,release", // the default the workflow passes through
		IncludeAAB:  true,            // ditto
		Library:     true,
		AARName:     "my-android-lib",
		DebugName:   "should-not-appear-debug",
		ReleaseName: "should-not-appear-release",
		AABName:     "should-not-appear-aab",
		Version:     "1.2.3",
		VersionCode: "42",
		Now:         time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	got := sink.buf.String()

	if !strings.Contains(got, "✓ Release AAR: `my-android-lib`") {
		t.Errorf("missing the AAR line in:\n%s", got)
	}

	for _, unwanted := range []string{
		"Debug APK",
		"Release APK",
		"Release AAB",
		"**Include AAB**",
		"should-not-appear",
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("library summary should not mention %q:\n%s", unwanted, got)
		}
	}

	if !strings.Contains(got, "| **Build Types** | release (library) |") {
		t.Errorf("library summary should report release-only build types:\n%s", got)
	}
}
