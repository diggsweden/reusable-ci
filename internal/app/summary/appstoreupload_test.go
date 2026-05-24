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

func TestAppStoreUpload_RendersHeaderAndTimestamp(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}
	err := appsummary.AppStoreUpload(context.Background(), sink, appsummary.AppStoreUploadInput{
		IPAFile:      "Demo.ipa",
		Platform:     "ios",
		SubmitReview: true,
		Now:          time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	got := sink.buf.String()
	require.Contains(t, got, "## App Store Connect Upload Summary 📱")
	require.Contains(t, got, "*Upload completed at 2026-05-10 14:00:00 UTC*")
}

func TestAppStoreUpload_ValidationAndManualGuidance(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}
	err := appsummary.AppStoreUpload(context.Background(), sink, appsummary.AppStoreUploadInput{
		IPAFile:        "app.ipa",
		Platform:       "ios",
		SkipValidation: true,
		SubmitReview:   false,
		RequestID:      "request-123",
		Now:            time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	got := sink.buf.String()
	for _, want := range []string{"app.ipa", "⊘ Skipped", "request-123", "Manually submit for external testing or App Store review"} {
		require.Contains(t, got, want)
	}
}
