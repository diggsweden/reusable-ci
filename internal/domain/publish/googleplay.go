// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
	"path/filepath"
	"strings"
	"time"
)

// ValidateGooglePlayServiceAccount reports whether body parses as a
// service-account JSON key with a parseable RSA private key. This does not
// establish that Google issued or still accepts the credential. It catches a wrong
// secret value (env var typo, copy-pasted GitHub token, etc.) without
// coupling to Google's evolving non-essential fields.
//
// Required fields:
//   - "type": "service_account"
//   - "client_email": non-empty
//   - "private_key": non-empty
func ValidateGooglePlayServiceAccount(body []byte) error { //nolint:cyclop // JSON fields, PEM framing, key parsing and RSA validity are separate credential checks.
	if len(body) == 0 {
		return fmt.Errorf("service account JSON is empty"+": %w", errs.ErrUsage)
	}

	var key struct {
		Type        string `json:"type"`
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
	}
	if err := json.Unmarshal(body, &key); err != nil {
		// The service account file is an operator-supplied secret. Malformed
		// content is EX_DATAERR (65) — the same class as the sibling
		// missing-field checks below — not the unclassified EX_SOFTWARE (70)
		// that tells them to file a bug against reusable-ci.
		return fmt.Errorf("parse service account JSON: expected service-account object: %w", errs.ErrMalformedInput)
	}

	if key.Type != "service_account" {
		return fmt.Errorf("service account JSON type: want \"service_account\": %w", errs.ErrValidation)
	}

	if strings.TrimSpace(key.ClientEmail) == "" {
		return fmt.Errorf("service account JSON is missing client_email"+": %w", errs.ErrMalformedInput)
	}

	if strings.TrimSpace(key.PrivateKey) == "" {
		return fmt.Errorf("service account JSON is missing private_key"+": %w", errs.ErrMalformedInput)
	}

	block, rest := pem.Decode([]byte(key.PrivateKey))
	if block == nil || block.Type != "PRIVATE KEY" || len(block.Headers) != 0 || strings.TrimSpace(string(rest)) != "" {
		return fmt.Errorf("service account private_key must be PKCS8 PEM: %w", errs.ErrMalformedInput)
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("service account private_key must contain a valid private key: %w", errs.ErrMalformedInput)
	}

	keyRSA, ok := parsed.(*rsa.PrivateKey)
	if !ok || keyRSA.Validate() != nil {
		return fmt.Errorf("service account private_key must contain a valid RSA private key: %w", errs.ErrMalformedInput)
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
	_, _ = fmt.Fprintf(&b, "| **AAB File** | %s |\n", summary.InlineCode(filepath.Base(in.AABFile)))
	_, _ = fmt.Fprintf(&b, "| **Package** | %s |\n", summary.InlineCode(in.PackageName))
	_, _ = fmt.Fprintf(&b, "| **Track** | %s |\n", summary.LiteralText(in.Track))
	_, _ = fmt.Fprintf(&b, "| **Status** | %s |\n", summary.LiteralText(in.Status))

	if in.ReleaseName != "" {
		_, _ = fmt.Fprintf(&b, "| **Release Name** | %s |\n", summary.LiteralText(in.ReleaseName))
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
		_, _ = fmt.Fprintf(&b, "2. Build will be available to %s testers after review\n", summary.LiteralText(in.Track))
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
