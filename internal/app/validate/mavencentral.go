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
// present and prints a confirmation line. Mirrors
// scripts/validate/mavencentral-credentials.sh exactly: missing values
// report which secret was missing, error message follows with the
// "Required for publishing to Maven Central" note.
func MavenCentralCredentials(stdout, stderr io.Writer, annot output.Annotator, in MavenCentralCredentialsInput) error {
	if in.Username == "" {
		annot.Errorf("Missing MAVEN_CENTRAL_USERNAME secret")
		fmt.Fprintln(stdout, "Required for publishing to Maven Central")
		return fmt.Errorf("missing MAVEN_CENTRAL_USERNAME secret: %w", errs.ErrPermissionDenied)
	}
	if in.Password == "" {
		annot.Errorf("Missing MAVEN_CENTRAL_PASSWORD secret")
		fmt.Fprintln(stdout, "Required for publishing to Maven Central")
		return fmt.Errorf("missing MAVEN_CENTRAL_PASSWORD secret: %w", errs.ErrPermissionDenied)
	}
	fmt.Fprintln(stdout, "✓ Maven Central credentials configured")
	return nil
}
