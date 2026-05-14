// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package publish_test

import (
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/publish"
)

func TestRenderAppStoreUploadSummary_Full(t *testing.T) {
	now := time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC)
	got := publish.RenderAppStoreUploadSummary(publish.AppStoreUploadInput{
		IPAFile:        "build/export/Demo.ipa",
		Platform:       "ios",
		SkipValidation: false,
		SubmitReview:   true,
		RequestID:      "abc-1234",
	}, now)
	for _, want := range []string{
		"## App Store Connect Upload Summary 📱",
		"| **IPA File** | `Demo.ipa` |",
		"| **Platform** | ios |",
		"| **Validation** | ✓ Passed |",
		"| **Status** | ✓ Uploaded |",
		"| **Request ID** | `abc-1234` |",
		"3. Build will be automatically submitted for App Store review",
		"*Upload completed at 2026-05-10 14:00:00 UTC*",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderAppStoreUploadSummary_SkippedValidation_ManualSubmission(t *testing.T) {
	got := publish.RenderAppStoreUploadSummary(publish.AppStoreUploadInput{
		IPAFile: "Demo.ipa", Platform: "ios", SkipValidation: true, SubmitReview: false,
	}, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC))
	if !strings.Contains(got, "| **Validation** | ⊘ Skipped |") {
		t.Errorf("missing skipped marker:\n%s", got)
	}
	if !strings.Contains(got, "3. Manually submit for external testing or App Store review from App Store Connect") {
		t.Errorf("missing manual-submit step:\n%s", got)
	}
}

func TestRenderAppStoreUploadSummary_OmitsRequestIDWhenEmpty(t *testing.T) {
	got := publish.RenderAppStoreUploadSummary(publish.AppStoreUploadInput{
		IPAFile: "Demo.ipa", Platform: "ios",
	}, time.Now())
	if strings.Contains(got, "Request ID") {
		t.Errorf("expected no request-id row:\n%s", got)
	}
}
