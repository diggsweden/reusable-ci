// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
)

func TestGooglePlayUpload_RendersStagedRolloutTable(t *testing.T) {
	t.Parallel()
	sink := &fakeSummarySink{}
	err := appsummary.GooglePlayUpload(context.Background(), sink, appsummary.GooglePlayUploadInput{
		AABFile:         "Demo.aab",
		PackageName:     "se.digg.demo",
		Track:           "production",
		Status:          "completed",
		UserFraction:    0.25,
		UserFractionSet: true,
		Priority:        5,
		Now:             time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	got := sink.buf.String()
	for _, want := range []string{
		"## Google Play Upload Summary",
		"| **Staged Rollout** | 25% |",
		"| **Update Priority** | 5 |",
		"2. Staged rollout to 25% of users will begin after review",
	} {
			require.Contains(t, got, want)
		}
}

func TestGooglePlayUpload_DraftGuidance(t *testing.T) {
	t.Parallel()
	sink := &fakeSummarySink{}
	err := appsummary.GooglePlayUpload(context.Background(), sink, appsummary.GooglePlayUploadInput{
		AABFile:         "app-release.aab",
		PackageName:     "se.digg.app",
		Track:           "production",
		Status:          "draft",
		ReleaseName:     "Release 1",
		UserFraction:    0.25,
		UserFractionSet: true,
		Priority:        3,
		Now:             time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	got := sink.buf.String()
	for _, want := range []string{"app-release.aab", "Staged rollout to 25% of users will begin after review", "Release is saved as draft"} {
		require.Contains(t, got, want)
	}
}
