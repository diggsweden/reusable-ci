// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	appsecurity "github.com/diggsweden/reusable-ci/v3/internal/app/security"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeprovider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
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
			SARIFFile:  filepath.Join(t.TempDir(), "missing.sarif"),
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
			// Every missing field is a broken invocation: ErrUsage, exit 2.
			// Without the sentinel these would exit 70 ("file a bug").
			require.ErrorIs(t, err, errs.ErrUsage)
			require.Contains(t, err.Error(), testCase.errContains)
			require.Empty(t, prov.UploadSARIFCalls(), "uploaded despite incomplete input")
		})
	}
}

// TestUploadSARIF_StampsEachRunAndLeavesTheFile covers a document with more
// than one run, which the happy path above cannot: it has no runs, so the
// category has nothing to stamp. The run without an analysis ID gets the
// category with its index; the run that declared its own keeps it. The
// payload is compared as a UseNumber tree, so a large number keeps its
// spelling, and the file on disk is not rewritten -- the category exists only
// in what is sent.
func TestUploadSARIF_StampsEachRunAndLeavesTheFile(t *testing.T) {
	t.Parallel()

	const body = `{"version":"2.1.0","runs":[
{"tool":{"driver":{"name":"trivy"}},"properties":{"rank":123456789012345678901234567890}},
{"tool":{"driver":{"name":"opengrep"}},"automationDetails":{"id":"producer/own"}}]}`

	path := writeTempSARIF(t, body)
	prov := fakeprovider.New(t)

	err := appsecurity.UploadSARIF(context.Background(), prov, io.Discard, output.Annotator{}, appsecurity.UploadSARIFInput{
		SARIFFile: path, Token: "t", Repository: "owner/repo",
		SHA: "0123456789abcdef0123456789abcdef01234567", Ref: "refs/heads/main", Category: "deps",
	})
	require.NoError(t, err)

	calls := prov.UploadSARIFCalls()
	require.Len(t, calls, 1)

	want := decodeSARIFTree(t, []byte(`{"version":"2.1.0","runs":[
{"tool":{"driver":{"name":"trivy"}},"properties":{"rank":123456789012345678901234567890},"automationDetails":{"id":"deps/0"}},
{"tool":{"driver":{"name":"opengrep"}},"automationDetails":{"id":"producer/own"}}]}`))

	require.Equal(t, want, decodeSARIFTree(t, calls[0].SARIF))

	onDisk, readErr := os.ReadFile(path)
	require.NoError(t, readErr)

	if !bytes.Equal(onDisk, []byte(body)) {
		t.Errorf("SARIF on disk changed:\n%s", onDisk)
	}
}

// TestUploadSARIF_FailureNeverShowsTheToken feeds a provider error that
// quotes the token. Neither the returned error nor the annotation may carry
// it, while the rest of the message and its error class survive -- a
// redaction that dropped everything would pass an absence check alone.
func TestUploadSARIF_FailureNeverShowsTheToken(t *testing.T) {
	t.Parallel()

	const token = "ghs_SyntheticUploadToken0123456789" //nolint:gosec // synthetic token that must never appear in output.

	prov := fakeprovider.New(t).WithUploadSARIFError(
		fmt.Errorf("HTTP 401: bad credentials for %s: %w", token, errs.ErrPermissionDenied))

	var stderr bytes.Buffer

	err := appsecurity.UploadSARIF(context.Background(), prov, io.Discard, output.NewAnnotator(&stderr, output.FormatGitHub), appsecurity.UploadSARIFInput{
		SARIFFile: writeTempSARIF(t, `{"runs":[]}`), Token: token, Repository: "owner/repo",
		SHA: "0123456789abcdef0123456789abcdef01234567", Ref: "refs/heads/main",
	})
	require.ErrorIs(t, err, errs.ErrPermissionDenied)

	for name, text := range map[string]string{"error": err.Error(), "annotation": stderr.String()} {
		require.NotContains(t, text, token, name)
		require.Contains(t, text, "HTTP 401: bad credentials for [redacted]", name)
	}
}

func decodeSARIFTree(t *testing.T, body []byte) map[string]any {
	t.Helper()

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	var doc map[string]any
	require.NoError(t, decoder.Decode(&doc))

	return doc
}
