// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package publish

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// GooglePlayUploadInput drives RenderGooglePlayUploadSummary.
type GooglePlayUploadInput struct {
	AABFile         string
	PackageName     string
	Track           string  // "internal" | "alpha" | "beta" | "production"
	Status          string  // "draft" | "completed" | "inProgress" | "halted"
	ReleaseName     string  // optional
	UserFraction    float64 // 0 → omit; >0 → render as "<n>%" rounded
	UserFractionSet bool    // distinguishes "not set" from "0.0"
	Priority        int     // 0 → omit row; non-zero → render
}

// RenderGooglePlayUploadSummary returns the markdown block written by
// scripts/summary/write-google-play-summary.sh, byte-for-byte.
func RenderGooglePlayUploadSummary(in GooglePlayUploadInput, now time.Time) string {
	percentage := ""
	if in.UserFractionSet {
		percentage = fmt.Sprintf("%.0f%%", in.UserFraction*100)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## Google Play Upload Summary\n\n")
	fmt.Fprintf(&b, "### Upload Details\n")
	fmt.Fprintf(&b, "| Property | Value |\n")
	fmt.Fprintf(&b, "|----------|-------|\n")
	fmt.Fprintf(&b, "| **AAB File** | `%s` |\n", filepath.Base(in.AABFile))
	fmt.Fprintf(&b, "| **Package** | `%s` |\n", in.PackageName)
	fmt.Fprintf(&b, "| **Track** | %s |\n", in.Track)
	fmt.Fprintf(&b, "| **Status** | %s |\n", in.Status)

	if in.ReleaseName != "" {
		fmt.Fprintf(&b, "| **Release Name** | %s |\n", in.ReleaseName)
	}
	if in.UserFractionSet {
		fmt.Fprintf(&b, "| **Staged Rollout** | %s |\n", percentage)
	}
	if in.Priority != 0 {
		fmt.Fprintf(&b, "| **Update Priority** | %d |\n", in.Priority)
	}

	fmt.Fprintf(&b, "| **Upload Status** | Uploaded |\n")

	fmt.Fprintf(&b, "\n### Next Steps\n")
	fmt.Fprintf(&b, "1. Check [Google Play Console](https://play.google.com/console) for upload status\n")
	switch in.Track {
	case "internal":
		fmt.Fprintf(&b, "2. Build will be available to internal testers within minutes\n")
	case "alpha", "beta":
		fmt.Fprintf(&b, "2. Build will be available to %s testers after review\n", in.Track)
	case "production":
		if percentage != "" {
			fmt.Fprintf(&b, "2. Staged rollout to %s of users will begin after review\n", percentage)
		} else {
			fmt.Fprintf(&b, "2. Full production release will begin after review\n")
		}
	}

	if in.Status == "draft" {
		fmt.Fprintf(&b, "3. Release is saved as draft - manually publish from Play Console when ready\n")
	}

	fmt.Fprintf(&b, "\n*Upload completed at %s*\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))
	return b.String()
}
