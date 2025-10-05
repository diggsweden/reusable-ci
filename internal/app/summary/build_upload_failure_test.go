// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestBuildUploadSummary_AppendFailureContract(t *testing.T) {
	t.Parallel()
	// ExtractedBinaries has the same contract in TestExtractedBinaries_FileListBoundaries.
	for _, tc := range []struct {
		name       string
		run        func(context.Context, ci.SummarySink) error
		wantReport []string
		prefix     string
	}{
		{
			name: "GoBuild",
			run: func(ctx context.Context, sink ci.SummarySink) error {
				return appsummary.GoBuild(ctx, sink, appsummary.GoBuildInput{Module: "example.test/contract", BinaryName: "contract-cli", Version: "2.4.6", Platforms: "linux/arm64"})
			},
			wantReport: []string{"## Go Build Summary\n", "- **Module:** example.test/contract\n", "- **Binary:** contract-cli\n"},
		},
		{
			name: "MavenBuild",
			run: func(ctx context.Context, sink ci.SummarySink) error {
				return appsummary.MavenBuild(ctx, sink, appsummary.MavenBuildInput{BuildType: "lib", GroupID: "se.digg.contract", ArtifactID: "maven-library", Version: "3.5.7", JavaVersion: "21", Now: fixedNow()})
			},
			wantReport: []string{"## Maven Build Summary \U0001f528\n", "- **Artifact:** `se.digg.contract:maven-library:3.5.7`\n", "*Build completed at 2026-05-10 14:30:00 UTC*\n"},
		},
		{
			name: "GradleBuild",
			run: func(ctx context.Context, sink ci.SummarySink) error {
				return appsummary.GradleBuild(ctx, sink, appsummary.GradleBuildInput{JavaVersion: "25", GradleTasks: ":contract:assemble", Version: "4.6.8", Now: fixedNow()})
			},
			wantReport: []string{"## Gradle Build Summary \U0001f528\n", "- **Tasks:** &#58;contract&#58;assemble\n", "*Build completed at 2026-05-10 14:30:00 UTC*\n"},
		},
		{
			name: "NPMBuild",
			run: func(ctx context.Context, sink ci.SummarySink) error {
				return appsummary.NPMBuild(ctx, sink, appsummary.NPMBuildInput{PackageName: "@contract/widget", Version: "5.7.9", NodeVersion: "24", Now: fixedNow()})
			},
			wantReport: []string{"## NPM Build Summary \U0001f528\n", "- **Package:** `@contract/widget@5.7.9`\n", "*Build completed at 2026-05-10 14:30:00 UTC*\n"},
		},
		{
			name: "AndroidBuild",
			run: func(ctx context.Context, sink ci.SummarySink) error {
				return appsummary.AndroidBuild(ctx, sink, appsummary.AndroidBuildInput{BuildModule: "contract-phone", BuildTypes: "debug", DebugName: "contract-debug", Now: fixedNow()})
			},
			wantReport: []string{"## Android Variants Build Summary \U0001f4f1\n", "| **Module** | contract-phone |\n", "\u2713 Debug APK: `contract-debug`\n", "*Build completed at 2026-05-10 14:30:00 UTC*\n"},
		},
		{
			name: "XcodeBuild",
			run: func(ctx context.Context, sink ci.SummarySink) error {
				return appsummary.XcodeBuild(ctx, sink, appsummary.XcodeBuildInput{XcodeVersion: "16.4", Scheme: "ContractTV", IPAName: "contract-tv", Signing: true, Now: fixedNow()})
			},
			wantReport: []string{"## Xcode Build Summary \U0001f4f1\n", "| **Scheme** | ContractTV |\n", "\u2713 IPA: `contract-tv`\n", "*Build completed at 2026-05-10 14:30:00 UTC*\n"},
		},
		{
			name: "GooglePlayUpload",
			run: func(ctx context.Context, sink ci.SummarySink) error {
				return appsummary.GooglePlayUpload(ctx, sink, appsummary.GooglePlayUploadInput{AABFile: "out/contract.aab", PackageName: "se.digg.contract", Track: "internal", Status: "completed", Now: fixedNow()})
			},
			wantReport: []string{"## Google Play Upload Summary\n", "| **AAB File** | `contract.aab` |\n", "| **Package** | `se.digg.contract` |\n", "*Upload completed at 2026-05-10 14:30:00 UTC*\n"},
		},
		{
			name: "AppStoreUpload",
			run: func(ctx context.Context, sink ci.SummarySink) error {
				return appsummary.AppStoreUpload(ctx, sink, appsummary.AppStoreUploadInput{IPAFile: "out/contract.ipa", Platform: "tvos", RequestID: "contract-request", Now: fixedNow()})
			},
			wantReport: []string{"## App Store Connect Upload Summary \U0001f4f1\n", "| **IPA File** | `contract.ipa` |\n", "| **Request ID** | `contract-request` |\n", "*Upload completed at 2026-05-10 14:30:00 UTC*\n"},
		},
		{
			name: "BuildSBOMStatus",
			run: func(ctx context.Context, sink ci.SummarySink) error {
				// Skipped generation reaches Append without filesystem discovery.
				return appsummary.BuildSBOMStatus(ctx, sink, appsummary.BuildSBOMStatusInput{Ecosystem: "npm", Outcome: "skipped"})
			},
			wantReport: []string{"### Build SBOM\n", "- \u2298 Generation disabled; release continues without a build SBOM\n"},
		},
		{
			name: "SwiftLintSummary",
			run: func(ctx context.Context, sink ci.SummarySink) error {
				return appsummary.SwiftLintSummary(ctx, sink, appsummary.SwiftLintSummaryInput{SwiftFormatEnabled: true, SwiftFormatResult: "success", SwiftLintEnabled: true, SwiftLintResult: "failure"})
			},
			wantReport: []string{"## Swift Linting Summary\n", "| swift-format | \u2713 Pass |\n", "| SwiftLint | \u2717 Fail |\n", "### \u2717 Linting failed\n"},
			prefix:     "append swift-lint summary: ",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			sentinel := errors.New(tc.name + " append sentinel") //nolint:err113 // Distinct error identity for each injected failure.

			var attempts []string

			sink := appendFailureSink(func(gotCtx context.Context, markdown string) error {
				if gotCtx != ctx {
					t.Error("Append did not receive the caller's exact context")
				}

				attempts = append(attempts, markdown)

				return sentinel
			})

			err := tc.run(ctx, sink)
			if !errors.Is(err, sentinel) || err.Error() != tc.prefix+sentinel.Error() {
				t.Errorf("append error = %v, want identity and text %q", err, tc.prefix+sentinel.Error())
			}

			if tc.prefix == "" && err != sentinel { //nolint:err113,errorlint // Direct wrappers must return the exact sink error.
				t.Errorf("append error = %v, want unchanged sentinel", err)
			}

			if errors.Is(err, errs.ErrValidation) {
				t.Errorf("append failure must not be classified as ErrValidation: %v", err)
			}

			if len(attempts) != 1 {
				t.Fatalf("Append attempts = %d, want exactly one", len(attempts))
			}

			for _, want := range tc.wantReport {
				if !strings.Contains(attempts[0], want) {
					t.Errorf("attempted report missing %q:\n%s", want, attempts[0])
				}
			}
		})
	}
}

func TestSwiftLintSummary_AppendSuccessPrecedesValidation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		lintResult string
		wantFailed bool
		wantReport string
	}{
		{"failing_lint", "failure", true, "| swift-format | \u2713 Pass |\n| SwiftLint | \u2717 Fail |\n\n### \u2717 Linting failed\nPlease fix the issues above.\n"},
		{"healthy_lint", "success", false, "| swift-format | \u2713 Pass |\n| SwiftLint | \u2713 Pass |\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			calls := 0
			sink := appendFailureSink(func(gotCtx context.Context, markdown string) error {
				calls++

				if gotCtx != ctx {
					t.Error("Append did not receive the caller's exact context")
				}

				want := "## Swift Linting Summary\n\n| Linter | Status |\n|--------|--------|\n" + tc.wantReport
				if markdown != want {
					t.Errorf("summary = %q, want %q", markdown, want)
				}

				return nil
			})
			err := appsummary.SwiftLintSummary(ctx, sink, appsummary.SwiftLintSummaryInput{
				SwiftFormatEnabled: true, SwiftFormatResult: "success", SwiftLintEnabled: true, SwiftLintResult: tc.lintResult,
			})

			if calls != 1 {
				t.Errorf("Append calls = %d, want exactly one before returning", calls)
			}

			if tc.wantFailed {
				if !errors.Is(err, errs.ErrValidation) || err.Error() != "swift linting failed: "+errs.ErrValidation.Error() {
					t.Errorf("error = %v, want lint validation error after successful append", err)
				}
			} else if err != nil {
				t.Errorf("healthy lint after successful append returned %v, want nil", err)
			}
		})
	}
}
