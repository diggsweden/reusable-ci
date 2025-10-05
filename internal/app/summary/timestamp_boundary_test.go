// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

// TestTimestampedSummaries_RenderUTCAndExplicitInputs gives every timestamped
// summary (build, upload, publish, pull request, prerequisites, release and
// snapshot release) a clock at UTC+02, where 00:30 local is 22:30 UTC
// on the previous day, so a renderer that formats the local time or drops the
// conversion shows the wrong date and hour. The other tests use UTC clocks,
// which cannot tell. Each row also passes non-default values (NPM skipping
// tests, Gradle running them, an explicit Xcode configuration and destination)
// and requires those rows verbatim.
func TestTimestampedSummaries_RenderUTCAndExplicitInputs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 10, 0, 30, 0, 0, time.FixedZone("UTC+02", 2*60*60))

	for name, tc := range map[string]struct {
		render func(context.Context, *fakeSummarySink) error
		want   []string
	}{
		"maven": {
			render: func(ctx context.Context, sink *fakeSummarySink) error {
				return appsummary.MavenBuild(ctx, sink, appsummary.MavenBuildInput{BuildType: "library", GroupID: "se.digg", ArtifactID: "lib", Version: "1.0.0", JavaVersion: "25", Now: now})
			},
			want: []string{"*Build completed at 2026-05-09 22:30:00 UTC*"},
		},
		"gradle executes tests": {
			render: func(ctx context.Context, sink *fakeSummarySink) error {
				return appsummary.GradleBuild(ctx, sink, appsummary.GradleBuildInput{JavaVersion: "21", GradleTasks: "build", SkipTests: false, Now: now})
			},
			want: []string{"- **Tests:** ✓ Executed", "*Build completed at 2026-05-09 22:30:00 UTC*"},
		},
		"npm skips tests": {
			render: func(ctx context.Context, sink *fakeSummarySink) error {
				return appsummary.NPMBuild(ctx, sink, appsummary.NPMBuildInput{PackageName: "web", Version: "2.0.0", NodeVersion: "22", SkipTests: true, Now: now})
			},
			want: []string{"- **Tests:** ⊘ Skipped", "*Build completed at 2026-05-09 22:30:00 UTC*"},
		},
		"android": {
			render: func(ctx context.Context, sink *fakeSummarySink) error {
				return appsummary.AndroidBuild(ctx, sink, appsummary.AndroidBuildInput{JavaVersion: "21", JDKDist: "Temurin", BuildModule: "app", BuildTypes: "release", ReleaseName: "app-release", Now: now})
			},
			want: []string{"*Build completed at 2026-05-09 22:30:00 UTC*"},
		},
		"xcode explicit configuration and destination": {
			render: func(ctx context.Context, sink *fakeSummarySink) error {
				return appsummary.XcodeBuild(ctx, sink, appsummary.XcodeBuildInput{
					XcodeVersion: "16.4", Scheme: "App", Configuration: "Staging", Destination: "platform=iOS Simulator,name=iPhone 16",
					Signing: true, Version: "3.1.4", BuildNumber: "159", IPAName: "app-3.1.4", Now: now,
				})
			},
			want: []string{
				"| **Configuration** | Staging |", "| **Destination** | platform=iOS Simulator,name=iPhone 16 |",
				"| **Version** | 3.1.4 (159) |", "*Build completed at 2026-05-09 22:30:00 UTC*",
			},
		},
		"google play": {
			render: func(ctx context.Context, sink *fakeSummarySink) error {
				return appsummary.GooglePlayUpload(ctx, sink, appsummary.GooglePlayUploadInput{AABFile: "app.aab", PackageName: "se.digg.app", Track: "internal", Status: "completed", Now: now})
			},
			want: []string{"*Upload completed at 2026-05-09 22:30:00 UTC*"},
		},
		"maven central publish": {
			render: func(ctx context.Context, sink *fakeSummarySink) error {
				return appsummary.MavenCentralPublish(ctx, sink, appsummary.MavenCentralPublishInput{Now: now})
			},
			want: []string{"*Published at 2026-05-09 22:30:00 UTC*"},
		},
		"forge packages publish": {
			render: func(ctx context.Context, sink *fakeSummarySink) error {
				return appsummary.ForgePackagesPublish(ctx, sink, appsummary.ForgePackagesPublishInput{Repository: "org/repo", PackageType: "npm", Now: now})
			},
			want: []string{"*Published at 2026-05-09 22:30:00 UTC*"},
		},
		"pull request": {
			render: func(ctx context.Context, sink *fakeSummarySink) error {
				return appsummary.PRSummary(ctx, sink, appsummary.PRSummaryInput{ProjectType: "go", Branch: "feat/x", Commit: "abcdef0", Actor: "bot", Now: now})
			},
			want: []string{"| **Checked At** | 2026-05-09 22:30:00 UTC |"},
		},
		"prerequisites": {
			render: func(ctx context.Context, sink *fakeSummarySink) error {
				return appsummary.Prerequisites(ctx, sink, nil, appsummary.PrerequisitesSummaryInput{Now: now})
			},
			want: []string{"*Generated at: 2026-05-09 22:30:00 UTC*"},
		},
		"release": {
			render: func(ctx context.Context, sink *fakeSummarySink) error {
				return appsummary.ReleaseSummary(ctx, sink, appsummary.ReleaseSummaryInput{ReleaseVersion: "v1.2.3", Now: now})
			},
			want: []string{"| **Released At** | 2026-05-09 22:30:00 UTC |"},
		},
		"snapshot release": {
			render: func(ctx context.Context, sink *fakeSummarySink) error {
				return appsummary.SnapshotReleaseSummary(ctx, sink, io.Discard, appsummary.SnapshotReleaseSummaryInput{ProjectType: projecttype.Go, Now: now})
			},
			want: []string{"| **Built At** | 2026-05-09 22:30:00 UTC |"},
		},
		"app store": {
			render: func(ctx context.Context, sink *fakeSummarySink) error {
				return appsummary.AppStoreUpload(ctx, sink, appsummary.AppStoreUploadInput{IPAFile: "app.ipa", Platform: "ios", Now: now})
			},
			want: []string{"2026-05-09 22:30:00 UTC"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sink := &fakeSummarySink{}
			require.NoError(t, tc.render(t.Context(), sink))

			got := sink.buf.String()
			for _, want := range tc.want {
				require.Contains(t, got, want)
			}

			require.NotContains(t, got, "2026-05-10")
		})
	}
}

// TestTimestampedSummaries_ZeroClockUsesTheCurrentUTCTime is the one
// intentional zero-clock case: with no clock given, the footer carries the
// wall time of the call, in UTC.
func TestTimestampedSummaries_ZeroClockUsesTheCurrentUTCTime(t *testing.T) {
	t.Parallel()

	before := time.Now().UTC().Truncate(time.Second)
	sink := &fakeSummarySink{}

	require.NoError(t, appsummary.GradleBuild(t.Context(), sink, appsummary.GradleBuildInput{JavaVersion: "21", GradleTasks: "build"}))

	after := time.Now().UTC()

	match := regexp.MustCompile(`\*Build completed at (\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) UTC\*`).FindStringSubmatch(sink.buf.String())
	require.Len(t, match, 2, sink.buf.String())

	stamp, err := time.Parse(time.DateTime, strings.TrimSpace(match[1]))
	require.NoError(t, err)
	require.False(t, stamp.Before(before) || stamp.After(after), "footer %s outside [%s, %s]", stamp, before, after)
}
