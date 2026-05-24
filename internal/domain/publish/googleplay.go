// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package publish

import (
	"encoding/json"
	"fmt"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"path/filepath"
	"strings"
	"time"
)

// ValidateGooglePlayServiceAccount reports whether body parses as a
// Google-issued service-account JSON key. Strict enough to catch a wrong
// secret value (env var typo, copy-pasted GitHub token, etc.) without
// coupling to Google's evolving non-essential fields.
//
// Required fields:
//   - "type": "service_account"
//   - "client_email": non-empty
//   - "private_key": non-empty
func ValidateGooglePlayServiceAccount(body []byte) error {
	if len(body) == 0 {
		return fmt.Errorf("service account JSON is empty"+": %w", errs.ErrUsage)
	}

	var key struct {
		Type        string `json:"type"`
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
	}
	if err := json.Unmarshal(body, &key); err != nil {
		return fmt.Errorf("parse service account JSON: %w", err)
	}

	if key.Type != "service_account" {
		return fmt.Errorf("service account JSON has type %q, want \"service_account\": %w", key.Type, errs.ErrValidation)
	}

	if strings.TrimSpace(key.ClientEmail) == "" {
		return fmt.Errorf("service account JSON is missing client_email"+": %w", errs.ErrMalformedInput)
	}

	if strings.TrimSpace(key.PrivateKey) == "" {
		return fmt.Errorf("service account JSON is missing private_key"+": %w", errs.ErrMalformedInput)
	}

	return nil
}

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

// RenderGooglePlayUploadSummary returns the markdown block written by.
func RenderGooglePlayUploadSummary(in GooglePlayUploadInput, now time.Time) string {
	percentage := ""
	if in.UserFractionSet {
		percentage = fmt.Sprintf("%.0f%%", in.UserFraction*100)
	}

	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "## Google Play Upload Summary\n\n")
	_, _ = fmt.Fprintf(&b, "### Upload Details\n")
	_, _ = fmt.Fprintf(&b, "| Property | Value |\n")
	_, _ = fmt.Fprintf(&b, "|----------|-------|\n")
	_, _ = fmt.Fprintf(&b, "| **AAB File** | `%s` |\n", filepath.Base(in.AABFile))
	_, _ = fmt.Fprintf(&b, "| **Package** | `%s` |\n", in.PackageName)
	_, _ = fmt.Fprintf(&b, "| **Track** | %s |\n", in.Track)
	_, _ = fmt.Fprintf(&b, "| **Status** | %s |\n", in.Status)

	if in.ReleaseName != "" {
		_, _ = fmt.Fprintf(&b, "| **Release Name** | %s |\n", in.ReleaseName)
	}

	if in.UserFractionSet {
		_, _ = fmt.Fprintf(&b, "| **Staged Rollout** | %s |\n", percentage)
	}

	if in.Priority != 0 {
		_, _ = fmt.Fprintf(&b, "| **Update Priority** | %d |\n", in.Priority)
	}

	_, _ = fmt.Fprintf(&b, "| **Upload Status** | Uploaded |\n")

	_, _ = fmt.Fprintf(&b, "\n### Next Steps\n")
	_, _ = fmt.Fprintf(&b, "1. Check [Google Play Console](https://play.google.com/console) for upload status\n")

	switch in.Track {
	case "internal":
		_, _ = fmt.Fprintf(&b, "2. Build will be available to internal testers within minutes\n")
	case "alpha", "beta":
		_, _ = fmt.Fprintf(&b, "2. Build will be available to %s testers after review\n", in.Track)
	case "production":
		if percentage != "" {
			_, _ = fmt.Fprintf(&b, "2. Staged rollout to %s of users will begin after review\n", percentage)
		} else {
			_, _ = fmt.Fprintf(&b, "2. Full production release will begin after review\n")
		}
	}

	if in.Status == "draft" {
		_, _ = fmt.Fprintf(&b, "3. Release is saved as draft - manually publish from Play Console when ready\n")
	}

	_, _ = fmt.Fprintf(&b, "\n*Upload completed at %s*\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))

	return b.String()
}
