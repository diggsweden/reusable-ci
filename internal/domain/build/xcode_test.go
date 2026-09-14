// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/build"
)

func TestRenderXcodeSummary_RendersMetadataLiterally(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, raw, text string }{
		{"heading injection", "demo\n\n## B8-INJECTED\n", "demo  &#35;&#35; B8-INJECTED "},
		{"inline syntax", "[link](https://evil.invalid) <b>&amp;</b> \\| `` *_~\t\r\n", "&#91;link&#93;(https&#58;//evil.invalid) &#60;b&#62;&#38;amp;&#60;/b&#62; &#92;&#124; &#96;&#96; &#42;&#95;&#126;   "},
	} {
		for _, signing := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/signing=%t", tc.name, signing), func(t *testing.T) {
				t.Parallel()

				in := build.XcodeSummaryInput{
					XcodeVersion: "xcode-" + tc.raw, Scheme: "scheme-" + tc.raw, Configuration: "config-" + tc.raw,
					Destination: "destination-" + tc.raw, Version: "version-" + tc.raw, BuildNumber: "number-" + tc.raw,
					IPAName: "ipa-" + tc.raw, Signing: signing,
				}
				status := "✓ Enabled"
				artifact := "✓ IPA: <code>ipa-" + tc.text + "</code>\n"

				if !signing {
					status = "⊘ Disabled"

					artifact = "✓ Archive: <code>ipa-" + tc.text + "-archive</code>\n"
					if tc.name == "heading injection" {
						artifact = "✓ Archive: `ipa-demo  ## B8-INJECTED -archive`\n"
					}
				}

				want := fmt.Sprintf("## Xcode Build Summary 📱\n\n### Configuration\n| Setting | Value |\n|---------|-------|\n| **Xcode** | xcode-%s |\n| **Scheme** | scheme-%s |\n| **Configuration** | config-%s |\n| **Destination** | destination-%s |\n| **Signing** | %s |\n| **Version** | version-%s (number-%s) |\n\n### Artifacts Generated\n%s\n*Build completed at 2026-05-10 14:00:00 UTC*\n", tc.text, tc.text, tc.text, tc.text, status, tc.text, tc.text, artifact)

				got := build.RenderXcodeSummary(in, time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC))
				if got != want {
					t.Errorf("summary = %q, want %q", got, want)
				}
			})
		}
	}
}

func TestRenderXcodeSummary_RawVersionOmission(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ version, row string }{
		{"", ""}, {"unknown", ""}, {" ", "| **Version** |   (42) |\n"}, {"\nunknown", "| **Version** |  unknown (42) |\n"},
	} {
		t.Run(fmt.Sprintf("version=%q", tc.version), func(t *testing.T) {
			t.Parallel()

			got := build.RenderXcodeSummary(build.XcodeSummaryInput{
				XcodeVersion: "16.4", Scheme: "App", Configuration: "Debug", Destination: "generic/platform=iOS",
				Version: tc.version, BuildNumber: "42", IPAName: "demo",
			}, time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC))

			want := "## Xcode Build Summary 📱\n\n### Configuration\n| Setting | Value |\n|---------|-------|\n| **Xcode** | 16.4 |\n| **Scheme** | App |\n| **Configuration** | Debug |\n| **Destination** | generic/platform=iOS |\n| **Signing** | ⊘ Disabled |\n" + tc.row + "\n### Artifacts Generated\n✓ Archive: `demo-archive`\n\n*Build completed at 2026-05-10 14:00:00 UTC*\n"
			if got != want {
				t.Errorf("summary = %q, want %q", got, want)
			}
		})
	}
}

func TestParseXcodeVersionFromPbxproj_FindsBothKeys(t *testing.T) {
	t.Parallel()

	body := `
		ROOT_BUILD_PHASE = (
			MARKETING_VERSION = 1.2.3;
			CURRENT_PROJECT_VERSION = 42;
		);
	`

	got := build.ParseXcodeVersionFromPbxproj(body)
	if got.Version != "1.2.3" || got.Build != "42" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("got %+v, want {1.2.3 42}", got)
	}
}

func TestParseXcodeVersionFromPbxproj_TakesFirstOccurrence(t *testing.T) {
	t.Parallel()

	body := `
		MARKETING_VERSION = 1.2.3;
		MARKETING_VERSION = 9.9.9;
		CURRENT_PROJECT_VERSION = 42;
		CURRENT_PROJECT_VERSION = 99;
	`

	got := build.ParseXcodeVersionFromPbxproj(body)
	if got.Version != "1.2.3" || got.Build != "42" {
		t.Errorf("got=%+v, want first matches 1.2.3/42", got)
	}
}

// TestParseXcodeVersionFromPbxproj_EachKeyTakesItsOwnFirstOccurrence orders
// the duplicates so each key's guard is actually reached. In the test above
// the scan returns as soon as both keys are set, so the second
// CURRENT_PROJECT_VERSION is never read and a missing first-wins guard on the
// build number could not fail it. A pbxproj carries one pair per build
// configuration, so duplicates are the normal case, not an edge.
func TestParseXcodeVersionFromPbxproj_EachKeyTakesItsOwnFirstOccurrence(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ body, version, build string }{
		"build duplicates first": {
			body:    "CURRENT_PROJECT_VERSION = 42;\nCURRENT_PROJECT_VERSION = 99;\nMARKETING_VERSION = 1.2.3;\n",
			version: "1.2.3", build: "42",
		},
		"marketing duplicates first": {
			body:    "MARKETING_VERSION = 1.2.3;\nMARKETING_VERSION = 9.9.9;\nCURRENT_PROJECT_VERSION = 42;\n",
			version: "1.2.3", build: "42",
		},
		"only the build number": {
			body:    "CURRENT_PROJECT_VERSION = \"42\";\n",
			version: "unknown", build: "42",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := build.ParseXcodeVersionFromPbxproj(tc.body)
			if got.Version != tc.version || got.Build != tc.build {
				t.Errorf("got %+v, want {%s %s}", got, tc.version, tc.build)
			}
		})
	}
}

func TestParseXcodeVersionFromPbxproj_DefaultsUnknownWhenMissing(t *testing.T) {
	t.Parallel()

	got := build.ParseXcodeVersionFromPbxproj("// nothing useful")
	if got.Version != "unknown" || got.Build != "unknown" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("got %+v, want both unknown", got)
	}
}

func TestParseXcodeVersionFromPbxproj_StripsQuotedValues(t *testing.T) {
	t.Parallel()

	for _, spacing := range []string{"", " ", "\t "} {
		body := "MARKETING_VERSION = \"1.2.3-beta\"" + spacing + ";\nCURRENT_PROJECT_VERSION = \"42\"" + spacing + ";\n"

		got := build.ParseXcodeVersionFromPbxproj(body)
		if got.Version != "1.2.3-beta" || got.Build != "42" {
			t.Errorf("spacing=%q got=%+v, want 1.2.3-beta/42", spacing, got)
		}
	}
}

func TestRenderXcodeSummary_SignedBuild(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC)

	got := build.RenderXcodeSummary(build.XcodeSummaryInput{
		XcodeVersion:  "16.4", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Scheme:        "App",  //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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
		"| **Destination** | generic/platform=iOS |",
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
	t.Parallel()

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
	t.Parallel()

	got := build.RenderXcodeSummary(build.XcodeSummaryInput{
		XcodeVersion: "16.4", Scheme: "App", Version: "unknown",
	}, time.Now())
	if strings.Contains(got, "**Version**") {
		t.Errorf("expected no version row:\n%s", got)
	}
}
