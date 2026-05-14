// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build_test

import (
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/build"
)

func TestParseXcodeVersionFromPbxproj_FindsBothKeys(t *testing.T) {
	body := `
		ROOT_BUILD_PHASE = (
			MARKETING_VERSION = 1.2.3;
			CURRENT_PROJECT_VERSION = 42;
		);
	`
	got := build.ParseXcodeVersionFromPbxproj(body)
	if got.Version != "1.2.3" || got.Build != "42" {
		t.Errorf("got %+v, want {1.2.3 42}", got)
	}
}

func TestParseXcodeVersionFromPbxproj_TakesFirstOccurrence(t *testing.T) {
	body := `
		MARKETING_VERSION = 1.2.3;
		MARKETING_VERSION = 9.9.9;
	`
	got := build.ParseXcodeVersionFromPbxproj(body)
	if got.Version != "1.2.3" {
		t.Errorf("version = %q, want first match 1.2.3", got.Version)
	}
}

func TestParseXcodeVersionFromPbxproj_DefaultsUnknownWhenMissing(t *testing.T) {
	got := build.ParseXcodeVersionFromPbxproj("// nothing useful")
	if got.Version != "unknown" || got.Build != "unknown" {
		t.Errorf("got %+v, want both unknown", got)
	}
}

func TestParseXcodeVersionFromPbxproj_StripsQuotedValues(t *testing.T) {
	body := `MARKETING_VERSION = "1.2.3-beta";`
	got := build.ParseXcodeVersionFromPbxproj(body)
	if got.Version != "1.2.3-beta" {
		t.Errorf("version = %q, want 1.2.3-beta", got.Version)
	}
}

func TestRenderXcodeSummary_SignedBuild(t *testing.T) {
	now := time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC)
	got := build.RenderXcodeSummary(build.XcodeSummaryInput{
		XcodeVersion:  "16.4",
		Scheme:        "App",
		Configuration: "Release",
		Destination:   "generic/platform=iOS",
		Signing:       true,
		Version:       "1.2.3",
		BuildNumber:   "42",
		IPAName:       "demo-1.2.3",
	}, now)
	for _, want := range []string{
		"## Xcode Build Summary 📱",
		"| **Xcode** | 16.4 |",
		"| **Scheme** | App |",
		"| **Configuration** | Release |",
		"| **Signing** | ✓ Enabled |",
		"| **Version** | 1.2.3 (42) |",
		"✓ IPA: `demo-1.2.3`",
		"*Build completed at 2026-05-10 14:00:00 UTC*",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderXcodeSummary_UnsignedFallsBackToArchive(t *testing.T) {
	got := build.RenderXcodeSummary(build.XcodeSummaryInput{
		XcodeVersion: "16.4", Scheme: "App", Signing: false, IPAName: "demo",
	}, time.Now())
	if !strings.Contains(got, "✓ Archive: `demo-archive`") {
		t.Errorf("missing archive marker:\n%s", got)
	}
	if strings.Contains(got, "✓ IPA:") {
		t.Errorf("did not expect IPA marker:\n%s", got)
	}
}

func TestRenderXcodeSummary_OmitsVersionWhenUnknown(t *testing.T) {
	got := build.RenderXcodeSummary(build.XcodeSummaryInput{
		XcodeVersion: "16.4", Scheme: "App", Version: "unknown",
	}, time.Now())
	if strings.Contains(got, "**Version**") {
		t.Errorf("expected no version row:\n%s", got)
	}
}
