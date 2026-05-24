// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package publish

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// AppStoreUploadInput drives RenderAppStoreUploadSummary.
type AppStoreUploadInput struct {
	IPAFile        string
	Platform       string
	SkipValidation bool
	SubmitReview   bool
	RequestID      string // optional
}

// IsAppStoreKeyID reports whether s is a syntactically valid App Store Connect
// API key identifier: 1–32 alphanumeric characters. Apple-issued IDs are
// 10-character mixed-case alphanumeric; this is the strictest check that
// accepts every documented format while rejecting punctuation, path
// separators, or anything that could escape a `AuthKey_<id>.p8` filename.
//
//nolint:cyclop // App Store key-ID validation: one branch per character-class rule.
func IsAppStoreKeyID(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}

	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		default:
			return false
		}
	}

	return true
}

// ParseAppStoreUploadRequestID extracts the request identifier shown in the
// upload summary from altool's JSON output.
func ParseAppStoreUploadRequestID(body []byte) (string, error) {
	var result struct {
		ProductErrors []struct {
			RequestID string `json:"requestId"`
		} `json:"product-errors"`
		SuccessMessage string `json:"success-message"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse App Store upload result: %w", err)
	}

	for _, productErr := range result.ProductErrors {
		requestID := strings.TrimSpace(productErr.RequestID)
		if requestID != "" {
			return requestID, nil
		}
	}

	if success := strings.TrimSpace(result.SuccessMessage); success != "" {
		return success, nil
	}

	return "unknown", nil
}

// RenderAppStoreUploadSummary returns the markdown block written after App
// Store Connect upload.
func RenderAppStoreUploadSummary(in AppStoreUploadInput, now time.Time) string {
	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "## App Store Connect Upload Summary 📱\n\n")
	_, _ = fmt.Fprintf(&b, "### Upload Details\n")
	_, _ = fmt.Fprintf(&b, "| Property | Value |\n")
	_, _ = fmt.Fprintf(&b, "|----------|-------|\n")
	_, _ = fmt.Fprintf(&b, "| **IPA File** | `%s` |\n", filepath.Base(in.IPAFile))
	_, _ = fmt.Fprintf(&b, "| **Platform** | %s |\n", in.Platform)

	if in.SkipValidation {
		_, _ = fmt.Fprintf(&b, "| **Validation** | ⊘ Skipped |\n")
	} else {
		_, _ = fmt.Fprintf(&b, "| **Validation** | ✓ Passed |\n")
	}

	_, _ = fmt.Fprintf(&b, "| **Status** | ✓ Uploaded |\n")

	if in.RequestID != "" {
		_, _ = fmt.Fprintf(&b, "| **Request ID** | `%s` |\n", in.RequestID)
	}

	_, _ = fmt.Fprintf(&b, "\n### Next Steps\n")
	_, _ = fmt.Fprintf(&b, "1. Check [App Store Connect](https://appstoreconnect.apple.com) for build processing status\n")
	_, _ = fmt.Fprintf(&b, "2. Build will be available in TestFlight within 10-15 minutes after processing completes\n")

	if in.SubmitReview {
		_, _ = fmt.Fprintf(&b, "3. Review submission was requested; submit the processed build manually from App Store Connect\n")
	} else {
		_, _ = fmt.Fprintf(&b, "3. Manually submit for external testing or App Store review from App Store Connect\n")
	}

	_, _ = fmt.Fprintf(&b, "\n*Upload completed at %s*\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))

	return b.String()
}
