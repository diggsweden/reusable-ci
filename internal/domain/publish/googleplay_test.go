// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish_test

import (
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/publish"
)

func TestRenderGooglePlayUploadSummary_ProductionWithStagedRollout(t *testing.T) {
	now := time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC)

	got := publish.RenderGooglePlayUploadSummary(publish.GooglePlayUploadInput{
		AABFile:         "build/release/Demo.aab",
		PackageName:     "se.digg.demo",
		Track:           "production",
		Status:          "completed", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		ReleaseName:     "1.2.3 (42)",
		UserFraction:    0.1,
		UserFractionSet: true,
		Priority:        3,
	}, now)
	for _, want := range []string{
		"## Google Play Upload Summary",
		"| **AAB File** | `Demo.aab` |",
		"| **Package** | `se.digg.demo` |",
		"| **Track** | production |",
		"| **Status** | completed |",
		"| **Release Name** | 1.2.3 (42) |",
		"| **Staged Rollout** | 10% |",
		"| **Update Priority** | 3 |",
		"| **Upload Status** | Uploaded |",
		"2. Staged rollout to 10% of users will begin after review",
		"*Upload completed at 2026-05-10 14:00:00 UTC*",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderGooglePlayUploadSummary_DraftAddsManualStep3(t *testing.T) {
	got := publish.RenderGooglePlayUploadSummary(publish.GooglePlayUploadInput{
		AABFile: "Demo.aab", PackageName: "p", Track: "internal", Status: "draft", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC))
	if !strings.Contains(got, "3. Release is saved as draft - manually publish from Play Console when ready") {
		t.Errorf("missing draft step:\n%s", got)
	}

	if !strings.Contains(got, "2. Build will be available to internal testers within minutes") {
		t.Errorf("missing internal next-step:\n%s", got)
	}
}

func TestRenderGooglePlayUploadSummary_AlphaBetaShowTrackName(t *testing.T) {
	for _, track := range []string{"alpha", "beta"} {
		got := publish.RenderGooglePlayUploadSummary(publish.GooglePlayUploadInput{
			AABFile: "Demo.aab", PackageName: "p", Track: track, Status: "completed",
		}, time.Now())

		want := "2. Build will be available to " + track + " testers after review"
		if !strings.Contains(got, want) {
			t.Errorf("track %s: missing %q in:\n%s", track, want, got)
		}
	}
}

func TestRenderGooglePlayUploadSummary_ProductionFullReleaseWhenNoFraction(t *testing.T) {
	got := publish.RenderGooglePlayUploadSummary(publish.GooglePlayUploadInput{
		AABFile: "Demo.aab", PackageName: "p", Track: "production", Status: "completed",
	}, time.Now())
	if !strings.Contains(got, "2. Full production release will begin after review") {
		t.Errorf("missing full-release step:\n%s", got)
	}

	if strings.Contains(got, "Staged Rollout") {
		t.Errorf("did not expect Staged Rollout row when fraction unset:\n%s", got)
	}
}

func TestRenderGooglePlayUploadSummary_OmitsOptionalRows(t *testing.T) {
	got := publish.RenderGooglePlayUploadSummary(publish.GooglePlayUploadInput{
		AABFile: "Demo.aab", PackageName: "p", Track: "internal", Status: "completed",
	}, time.Now())
	if strings.Contains(got, "Release Name") {
		t.Errorf("expected no Release Name row:\n%s", got)
	}

	if strings.Contains(got, "Update Priority") {
		t.Errorf("expected no Update Priority row when zero:\n%s", got)
	}
}

func TestValidateGooglePlayServiceAccount_Accepts(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"type": "service_account",
		"client_email": "play-publisher@example.iam.gserviceaccount.com",
		"private_key": "-----BEGIN PRIVATE KEY-----\nbody\n-----END PRIVATE KEY-----\n"
	}`)
	if err := publish.ValidateGooglePlayServiceAccount(body); err != nil {
		t.Fatal(err)
	}
}

func TestValidateGooglePlayServiceAccount_Rejects(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{name: "empty body", body: "", want: "is empty"},
		{name: "not json", body: "not json", want: "parse service account JSON"},
		{name: "wrong type", body: `{"type":"user","client_email":"x@y","private_key":"k"}`, want: `type "user"`},
		{name: "missing client_email", body: `{"type":"service_account","private_key":"k"}`, want: "missing client_email"},
		{name: "missing private_key", body: `{"type":"service_account","client_email":"x@y"}`, want: "missing private_key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := publish.ValidateGooglePlayServiceAccount([]byte(tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
}
