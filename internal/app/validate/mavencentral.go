// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate

import (
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
)

// MavenCentralCredentialsInput drives MavenCentralCredentials.
type MavenCentralCredentialsInput struct {
	Username string
	Password string
}

// MavenCentralCredentials checks both Maven Central secrets are
// present and prints a confirmation line. Missing values report which
// secret was missing; the error message includes the "Required for
// publishing to Maven Central" hint.
func MavenCentralCredentials(w, stderr io.Writer, annot output.Annotator, in MavenCentralCredentialsInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if in.Username == "" {
		annot.Errorf("Missing MAVEN_CENTRAL_USERNAME secret")

		_, _ = fmt.Fprintln(w, "Required for publishing to Maven Central")

		return fmt.Errorf("missing MAVEN_CENTRAL_USERNAME secret: %w", errs.ErrPermissionDenied)
	}

	if in.Password == "" {
		annot.Errorf("Missing MAVEN_CENTRAL_PASSWORD secret")

		_, _ = fmt.Fprintln(w, "Required for publishing to Maven Central")

		return fmt.Errorf("missing MAVEN_CENTRAL_PASSWORD secret: %w", errs.ErrPermissionDenied)
	}

	_, _ = fmt.Fprintln(w, "✓ Maven Central credentials configured")

	return nil
}
