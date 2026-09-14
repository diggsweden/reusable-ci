// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
)

func TestGooglePlayUpload_ForwardsAllFields(t *testing.T) {
	t.Parallel()

	const (
		heading   = "## Google Play Upload Summary\n\n### Upload Details\n| Property | Value |\n|----------|-------|\n"
		nextSteps = "| **Upload Status** | Uploaded |\n\n### Next Steps\n1. Check [Google Play Console](https://play.google.com/console) for upload status\n"
		footer    = "\n*Upload completed at 2026-05-10 14:30:00 UTC*\n"
	)

	for _, tc := range []struct {
		name     string
		in       appsummary.GooglePlayUploadInput
		wantRows string
		wantNext string
	}{
		{
			name: "staged_draft",
			in: appsummary.GooglePlayUploadInput{
				AABFile: "out/phone/store-release.aab", PackageName: "se.digg.phone", Track: "production", Status: "draft",
				ReleaseName: "Phone 2.4", UserFraction: 0.37, UserFractionSet: true, Priority: 4,
			},
			wantRows: "| **AAB File** | `store-release.aab` |\n| **Package** | `se.digg.phone` |\n" +
				"| **Track** | production |\n| **Status** | draft |\n| **Release Name** | Phone 2.4 |\n" +
				"| **Staged Rollout** | 37% |\n| **Update Priority** | 4 |\n",
			wantNext: "2. Staged rollout to 37% of users will begin after review\n3. Release is saved as draft - manually publish from Play Console when ready\n",
		},
		{
			name: "nonzero_fraction_not_set",
			in: appsummary.GooglePlayUploadInput{
				AABFile: "build/tablet/beta.aab", PackageName: "se.digg.tablet", Track: "beta", Status: "completed",
				ReleaseName: "", UserFraction: 0.61, UserFractionSet: false, Priority: 0,
			},
			wantRows: "| **AAB File** | `beta.aab` |\n| **Package** | `se.digg.tablet` |\n" +
				"| **Track** | beta |\n| **Status** | completed |\n",
			wantNext: "2. Build will be available to beta testers after review\n",
		},
		{
			name: "zero_fraction_explicitly_set",
			in: appsummary.GooglePlayUploadInput{
				AABFile: "artifacts/watch/watch.aab", PackageName: "se.digg.watch", Track: "production", Status: "inProgress",
				ReleaseName: "Watch 3.5", UserFraction: 0, UserFractionSet: true, Priority: 2,
			},
			wantRows: "| **AAB File** | `watch.aab` |\n| **Package** | `se.digg.watch` |\n" +
				"| **Track** | production |\n| **Status** | inProgress |\n| **Release Name** | Watch 3.5 |\n" +
				"| **Staged Rollout** | 0% |\n| **Update Priority** | 2 |\n",
			wantNext: "2. Staged rollout to 0% of users will begin after review\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sink := &fakeSummarySink{}
			tc.in.Now = fixedNow()
			require.NoError(t, appsummary.GooglePlayUpload(t.Context(), sink, tc.in))
			require.Equal(t, heading+tc.wantRows+nextSteps+tc.wantNext+footer, sink.buf.String())
		})
	}
}
