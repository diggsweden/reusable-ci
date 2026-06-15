// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	appsecurity "github.com/diggsweden/reusable-ci/internal/app/security"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeprovider"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

// writeTempSARIF creates a SARIF file in an isolated test filesystem.
func writeTempSARIF(t *testing.T, body string) string {
	t.Helper()

	return testfs.NewReal(t).WriteFile("results.sarif", []byte(body))
}

func TestUploadSARIF_HappyPath_ForwardsPayloadToProvider(t *testing.T) {
	t.Parallel()

	const sarifBody = `{"version":"2.1.0","runs":[]}`

	sarif := writeTempSARIF(t, sarifBody)

	prov := fakeprovider.New(t)

	var out, stderr bytes.Buffer

	err := appsecurity.UploadSARIF(context.Background(), prov, &out,
		output.NewAnnotator(&stderr, output.FormatGitHub),
		appsecurity.UploadSARIFInput{
			SARIFFile:  sarif,
			Token:      "secret",
			Repository: "diggsweden/reusable-ci",
			SHA:        "abcdef0123456789",
			Ref:        "refs/heads/main",
			Category:   "opengrep",
		})
	require.NoError(t, err, "stderr: %s", stderr.String())

	calls := prov.UploadSARIFCalls()
	require.Len(t, calls, 1)
	call := calls[0]
	require.Equal(t, "diggsweden/reusable-ci", call.Repository)
	require.Equal(t, "abcdef0123456789", call.SHA)
	require.Equal(t, "refs/heads/main", call.Ref)
	require.Equal(t, "secret", call.Token)
	// Empty runs: the category has nothing to stamp, so the body is
	// semantically unchanged. (automationDetails stamping is covered by the
	// domain SetSARIFCategory tests.)
	require.JSONEq(t, sarifBody, string(call.SARIF))
	require.Contains(t, out.String(), "✓ SARIF accepted by Code Scanning")
}

func TestUploadSARIF_NoTokenSkipsGracefully(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t)

	var stderr bytes.Buffer

	err := appsecurity.UploadSARIF(context.Background(), prov, io.Discard,
		output.NewAnnotator(&stderr, output.FormatGitHub),
		appsecurity.UploadSARIFInput{
			SARIFFile:  writeTempSARIF(t, `{}`),
			Repository: "x/y", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			SHA:        "s",
			Ref:        "r",
		})
	require.NoError(t, err)
	require.Empty(t, prov.UploadSARIFCalls(), "provider should not be called when token is missing")
	require.Contains(t, stderr.String(), "::notice::SARIF upload to Code Scanning skipped")
}

func TestUploadSARIF_MissingFileSkipsGracefully(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t)

	var stderr bytes.Buffer

	err := appsecurity.UploadSARIF(context.Background(), prov, io.Discard,
		output.NewAnnotator(&stderr, output.FormatGitHub),
		appsecurity.UploadSARIFInput{
			SARIFFile:  "/nonexistent/results.sarif",
			Token:      "t",
			Repository: "x/y",
			SHA:        "s",
			Ref:        "r",
		})
	require.NoError(t, err)
	require.Empty(t, prov.UploadSARIFCalls())
	require.Contains(t, stderr.String(), "::notice::SARIF file not found")
}

func TestUploadSARIF_ProviderErrorPropagates(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t).WithUploadSARIFError(
		fmt.Errorf("HTTP 422: validation failed: %w", errs.ErrPermissionDenied))

	var stderr bytes.Buffer

	err := appsecurity.UploadSARIF(context.Background(), prov, io.Discard,
		output.NewAnnotator(&stderr, output.FormatGitHub),
		appsecurity.UploadSARIFInput{
			SARIFFile:  writeTempSARIF(t, `{}`),
			Token:      "t",
			Repository: "x/y",
			SHA:        "s",
			Ref:        "r",
		})
	require.Error(t, err)
	require.ErrorIs(t, err, errs.ErrPermissionDenied)
	require.Contains(t, stderr.String(), "::error::SARIF upload failed")
}

func TestUploadSARIF_MissingRequiredFieldErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		given       appsecurity.UploadSARIFInput
		errContains string
	}{
		{
			name:        "missing_sarif_file",
			given:       appsecurity.UploadSARIFInput{Token: "t"},
			errContains: "SARIF_FILE",
		},
		{
			name:        "missing_repository",
			given:       appsecurity.UploadSARIFInput{Token: "t", SARIFFile: writeTempSARIF(t, "{}"), SHA: "s", Ref: "r"},
			errContains: "GITHUB_REPOSITORY",
		},
		{
			name:        "missing_sha",
			given:       appsecurity.UploadSARIFInput{Token: "t", SARIFFile: writeTempSARIF(t, "{}"), Repository: "x/y", Ref: "r"},
			errContains: "GITHUB_SHA",
		},
		{
			name:        "missing_ref",
			given:       appsecurity.UploadSARIFInput{Token: "t", SARIFFile: writeTempSARIF(t, "{}"), Repository: "x/y", SHA: "s"},
			errContains: "GITHUB_REF",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			prov := fakeprovider.New(t)
			err := appsecurity.UploadSARIF(context.Background(), prov, io.Discard, output.Annotator{}, testCase.given)
			require.Error(t, err)
			require.Contains(t, err.Error(), testCase.errContains)
		})
	}
}
