// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish

import (
	"context"
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	domainpublish "github.com/diggsweden/reusable-ci/internal/domain/publish"
)

// GooglePlayCheckCredentialsInput drives GooglePlayCheckCredentials.
type GooglePlayCheckCredentialsInput struct {
	// ServiceAccountJSON is the service-account JSON key body. The CLI
	// layer is responsible for sourcing it (typically from an env var
	// like GOOGLE_PLAY_SERVICE_ACCOUNT_JSON) so this layer is free of
	// I/O and trivially testable.
	ServiceAccountJSON string
}

// GooglePlayCheckCredentials validates that the supplied service account
// secret is a Google-issued service-account JSON key. Replaces the inline
// shell secret-presence check in publish-google-play.yml with a stricter
// shape check that catches "wrong secret pasted" before the upload action
// fails with a less actionable error.
func GooglePlayCheckCredentials(_ context.Context, w io.Writer, in GooglePlayCheckCredentialsInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if in.ServiceAccountJSON == "" {
		return fmt.Errorf("google Play service account JSON is required: %w", errs.ErrMissingInput)
	}

	if err := domainpublish.ValidateGooglePlayServiceAccount([]byte(in.ServiceAccountJSON)); err != nil {
		return fmt.Errorf("service account: %w: %w", err, errs.ErrValidation)
	}

	_, _ = fmt.Fprintln(w, "Service account secret is configured")

	return nil
}
