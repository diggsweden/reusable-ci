// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/publish"
)

func TestRenderAppStoreUploadSummary_Full(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC)

	got := publish.RenderAppStoreUploadSummary(publish.AppStoreUploadInput{
		IPAFile:        "build/export/Demo.ipa",
		Platform:       "ios", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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
		"3. Review submission was requested; submit the processed build manually from App Store Connect",
		"*Upload completed at 2026-05-10 14:00:00 UTC*",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestParseAppStoreUploadRequestID_PrefersProductErrorRequestID(t *testing.T) {
	t.Parallel()

	got, err := publish.ParseAppStoreUploadRequestID([]byte(`{"product-errors":[{"requestId":"req-123"}],"success-message":"ok"}`))
	if err != nil {
		t.Fatal(err)
	}

	if got != "req-123" {
		t.Errorf("request id = %q", got)
	}
}

func TestParseAppStoreUploadRequestID_FallsBackToSuccessMessage(t *testing.T) {
	t.Parallel()

	got, err := publish.ParseAppStoreUploadRequestID([]byte(`{"success-message":"success-123"}`))
	if err != nil {
		t.Fatal(err)
	}

	if got != "success-123" {
		t.Errorf("request id = %q", got)
	}
}

func TestParseAppStoreUploadRequestID_UnknownWhenAbsent(t *testing.T) {
	t.Parallel()

	got, err := publish.ParseAppStoreUploadRequestID([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}

	if got != "unknown" {
		t.Errorf("request id = %q", got)
	}
}

func TestParseAppStoreUploadRequestID_RejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := publish.ParseAppStoreUploadRequestID([]byte(`not json`))
	// altool's output is data from an external tool, so malformed output is
	// EX_DATAERR (65). Unclassified it exited EX_SOFTWARE (70), telling the
	// operator to file a bug against reusable-ci for Apple's output.
	if !errors.Is(err, errs.ErrMalformedInput) {
		t.Fatalf("err = %v, want ErrMalformedInput", err)
	}

	// The decoder's own message survives, so the failure is diagnosable.
	if !strings.Contains(err.Error(), "parse App Store upload result") {
		t.Errorf("err = %v, want it to name the operation", err)
	}
}

func TestRenderAppStoreUploadSummary_SkippedValidation_ManualSubmission(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	got := publish.RenderAppStoreUploadSummary(publish.AppStoreUploadInput{
		IPAFile: "Demo.ipa", Platform: "ios",
	}, time.Now())
	if strings.Contains(got, "Request ID") {
		t.Errorf("expected no request-id row:\n%s", got)
	}
}

// TestRenderAppStoreUploadSummary_ExternalValuesCannotChangeTheTable feeds the
// values that come from outside -- the IPA file name, the platform flag and
// altool's request ID, which falls back to its free-text success message --
// with pipes, backticks and newlines. The request ID used to sit inside raw
// backticks, so a backtick in altool's message closed the code span and a
// newline started a new table row.
func TestRenderAppStoreUploadSummary_ExternalValuesCannotChangeTheTable(t *testing.T) {
	t.Parallel()

	got := publish.RenderAppStoreUploadSummary(publish.AppStoreUploadInput{
		IPAFile:   "build/export/De`mo|x.ipa",
		Platform:  "ios\n| **Injected** | yes |",
		RequestID: "No errors uploading `App.ipa`\n| **Signed** | ✓ |",
	}, time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC))

	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "| **Injected**") || strings.HasPrefix(line, "| **Signed**") {
			t.Errorf("an external value forged a table row %q:\n%s", line, got)
		}
	}

	// Every table row still has exactly the two cells it was built with.
	for _, line := range strings.Split(got, "\n") {
		if !strings.HasPrefix(line, "| **") {
			continue
		}

		if cells := strings.Count(line, " | "); cells != 1 {
			t.Errorf("row %q has %d cell separators, want 1", line, cells)
		}
	}
}

// TestIsAppStoreKeyID_LengthEdge pins the bound the AuthKey_<id>.p8 filename
// rests on: 32 alphanumerics are accepted, 33 are not, and one is enough.
func TestIsAppStoreKeyID_LengthEdge(t *testing.T) {
	t.Parallel()

	for id, want := range map[string]bool{
		strings.Repeat("A", 32):       true,
		strings.Repeat("A", 33):       false,
		"a":                           true,
		"AB12CD34EF":                  true,
		"AB12CD34E/":                  false,
		strings.Repeat("A", 31) + "-": false,
	} {
		if got := publish.IsAppStoreKeyID(id); got != want {
			t.Errorf("IsAppStoreKeyID(%q) = %v, want %v", id, got, want)
		}
	}
}

// TestParseAppStoreUploadRequestID_SkipsABlankProductError: altool can list
// a product error without a request id before the one that carries it. The
// blank is skipped rather than returned as the empty string or as "unknown".
func TestParseAppStoreUploadRequestID_SkipsABlankProductError(t *testing.T) {
	t.Parallel()

	got, err := publish.ParseAppStoreUploadRequestID([]byte(`{"product-errors":[{"requestId":"  "},{"requestId":"req-456"}],"success-message":"ok"}`))
	if err != nil || got != "req-456" {
		t.Errorf("request id = %q, %v; want the later populated one", got, err)
	}
}
