// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package publish

import (
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

// RenderAppStoreUploadSummary returns the markdown block written by
// scripts/summary/write-appstore-summary.sh, byte-for-byte.
func RenderAppStoreUploadSummary(in AppStoreUploadInput, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## App Store Connect Upload Summary 📱\n\n")
	fmt.Fprintf(&b, "### Upload Details\n")
	fmt.Fprintf(&b, "| Property | Value |\n")
	fmt.Fprintf(&b, "|----------|-------|\n")
	fmt.Fprintf(&b, "| **IPA File** | `%s` |\n", filepath.Base(in.IPAFile))
	fmt.Fprintf(&b, "| **Platform** | %s |\n", in.Platform)
	if in.SkipValidation {
		fmt.Fprintf(&b, "| **Validation** | ⊘ Skipped |\n")
	} else {
		fmt.Fprintf(&b, "| **Validation** | ✓ Passed |\n")
	}
	fmt.Fprintf(&b, "| **Status** | ✓ Uploaded |\n")
	if in.RequestID != "" {
		fmt.Fprintf(&b, "| **Request ID** | `%s` |\n", in.RequestID)
	}

	fmt.Fprintf(&b, "\n### Next Steps\n")
	fmt.Fprintf(&b, "1. Check [App Store Connect](https://appstoreconnect.apple.com) for build processing status\n")
	fmt.Fprintf(&b, "2. Build will be available in TestFlight within 10-15 minutes after processing completes\n")
	if in.SubmitReview {
		fmt.Fprintf(&b, "3. Build will be automatically submitted for App Store review\n")
	} else {
		fmt.Fprintf(&b, "3. Manually submit for external testing or App Store review from App Store Connect\n")
	}
	fmt.Fprintf(&b, "\n*Upload completed at %s*\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))
	return b.String()
}
