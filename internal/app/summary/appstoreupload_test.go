// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
)

func TestAppStoreUpload_ForwardsAllFields(t *testing.T) {
	t.Parallel()

	const (
		heading   = "## App Store Connect Upload Summary 📱\n\n### Upload Details\n| Property | Value |\n|----------|-------|\n"
		nextSteps = "\n### Next Steps\n" +
			"1. Check [App Store Connect](https://appstoreconnect.apple.com) for build processing status\n" +
			"2. Build will be available in TestFlight within 10-15 minutes after processing completes\n"
		footer = "\n*Upload completed at 2026-05-10 14:30:00 UTC*\n"
	)

	for _, tc := range []struct {
		name     string
		in       appsummary.AppStoreUploadInput
		wantRows string
		wantNext string
	}{
		{
			name: "validated_tvos_with_review_request",
			in: appsummary.AppStoreUploadInput{
				IPAFile: "out/television/TV.ipa", Platform: "tvos", SkipValidation: false, SubmitReview: true, RequestID: "tv-request-246",
			},
			wantRows: "| **IPA File** | `TV.ipa` |\n| **Platform** | tvos |\n| **Validation** | ✓ Passed |\n" +
				"| **Status** | ✓ Uploaded |\n| **Request ID** | `tv-request-246` |\n",
			wantNext: "3. Review submission was requested; submit the processed build manually from App Store Connect\n",
		},
		{
			name: "macos_validation_skipped_without_review_request",
			in: appsummary.AppStoreUploadInput{
				IPAFile: "build/desktop/Desktop.pkg", Platform: "macos", SkipValidation: true, SubmitReview: false, RequestID: "mac-request-357",
			},
			wantRows: "| **IPA File** | `Desktop.pkg` |\n| **Platform** | macos |\n| **Validation** | ⊘ Skipped |\n" +
				"| **Status** | ✓ Uploaded |\n| **Request ID** | `mac-request-357` |\n",
			wantNext: "3. Manually submit for external testing or App Store review from App Store Connect\n",
		},
		{
			name: "validated_without_review_or_request_id",
			in: appsummary.AppStoreUploadInput{
				IPAFile: "artifacts/reader/Reader.pkg", Platform: "macos", SkipValidation: false, SubmitReview: false, RequestID: "",
			},
			wantRows: "| **IPA File** | `Reader.pkg` |\n| **Platform** | macos |\n| **Validation** | ✓ Passed |\n| **Status** | ✓ Uploaded |\n",
			wantNext: "3. Manually submit for external testing or App Store review from App Store Connect\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sink := &fakeSummarySink{}
			tc.in.Now = fixedNow()
			require.NoError(t, appsummary.AppStoreUpload(t.Context(), sink, tc.in))
			require.Equal(t, heading+tc.wantRows+nextSteps+tc.wantNext+footer, sink.buf.String())
		})
	}
}
