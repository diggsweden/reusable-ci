// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	appvalidate "github.com/diggsweden/reusable-ci/internal/app/validate"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
)

func TestMavenCentralCredentials_OK(t *testing.T) {
	t.Parallel()

	var out, stderr bytes.Buffer

	err := appvalidate.MavenCentralCredentials(&out, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appvalidate.MavenCentralCredentialsInput{
		Username: "u",
		Password: "p",
	})
	require.NoError(t, err)
	require.Contains(t, out.String(), "✓ Maven Central credentials configured")
}

func TestMavenCentralCredentials_MissingUsername(t *testing.T) {
	t.Parallel()

	var out, stderr bytes.Buffer

	err := appvalidate.MavenCentralCredentials(&out, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appvalidate.MavenCentralCredentialsInput{
		Password: "p",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "USERNAME")
	require.Contains(t, stderr.String(), "::error::Missing MAVEN_CENTRAL_USERNAME")
	require.Contains(t, out.String(), "Required for publishing to Maven Central")
}

func TestMavenCentralCredentials_MissingPassword(t *testing.T) {
	t.Parallel()

	var out, stderr bytes.Buffer

	err := appvalidate.MavenCentralCredentials(&out, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appvalidate.MavenCentralCredentialsInput{
		Username: "u",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "PASSWORD")
	require.Contains(t, out.String(), "Required for publishing to Maven Central")
}
