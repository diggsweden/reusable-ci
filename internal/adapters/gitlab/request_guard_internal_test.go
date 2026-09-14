// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/stretchr/testify/require"
)

type guardTransport func(*http.Request) (*http.Response, error)

func (f guardTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func requestGuardClient(t *testing.T, outcome, token string) (*http.Client, *int, error) {
	t.Helper()

	calls := 0
	cause := fmt.Errorf("reflected %s: %w", token, errs.ErrPermissionDenied)
	client := &http.Client{Transport: guardTransport(func(req *http.Request) (*http.Response, error) {
		calls++

		if req.Body != nil {
			_, _ = io.Copy(io.Discard, req.Body)
		}

		require.Equal(t, token, req.Header.Get("PRIVATE-TOKEN"))
		require.Equal(t, token, req.Header.Get("JOB-TOKEN"))

		if outcome == "transport" {
			return nil, cause
		}

		header := make(http.Header)

		status, body := 403, token
		if strings.Contains(outcome, "redirect") {
			status, body = 200, `{}`
			if calls == 1 {
				status = 302

				destination := "https://gitlab.invalid/next"
				if outcome == "foreign redirect" {
					destination = "https://other.invalid/next"
				}

				header.Set("Location", destination)
			}
		}

		return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}

	return client, &calls, cause
}

func TestRequestHelpers_ContainRedirectsAndCredentials(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "asset")
	require.NoError(t, os.WriteFile(file, []byte("fixture"), 0o600))

	for _, operation := range []string{"get", "post", "put", "delete", "multipart"} {
		for _, outcome := range []string{"reflection", "transport", "foreign redirect", "same redirect"} {
			t.Run(operation+"/"+outcome, func(t *testing.T) {
				t.Parallel()

				const token = "synthetic-header-secret"

				client, calls, cause := requestGuardClient(t, outcome, token)
				headers := map[string]string{"PRIVATE-TOKEN": token, "JOB-TOKEN": token}
				endpoint := "https://gitlab.invalid/start?credential=" + token

				var err error

				switch operation {
				case "get":
					_, err = getJSON(t.Context(), client, endpoint, headers)
				case "post":
					err = postJSON(t.Context(), client, endpoint, headers, []byte(`{}`))
				case "put":
					err = putJSON(t.Context(), client, endpoint, headers, []byte(`{}`))
				case "delete":
					err = deleteJSON(t.Context(), client, endpoint, headers)
				case "multipart":
					_, err = postMultipartFile(t.Context(), client, endpoint, headers, "file", file)
				}

				if outcome == "same redirect" {
					require.NoError(t, err)
					require.Equal(t, 2, *calls)
				} else {
					require.Error(t, err)
					require.NotContains(t, err.Error(), token)
					require.Equal(t, 1, *calls)
				}

				if outcome == "foreign redirect" {
					require.ErrorIs(t, err, errs.ErrValidation)
				}

				if outcome == "reflection" {
					require.ErrorIs(t, err, errs.ErrPermissionDenied)
					require.Contains(t, err.Error(), "403")
				}

				if outcome == "transport" {
					require.ErrorIs(t, err, cause)
				}

				require.Nil(t, client.CheckRedirect)
			})
		}
	}
}

func TestReleaseLocalPreflight_StopsBeforeGitLabRequests(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	good := filepath.Join(root, "good")
	require.NoError(t, os.WriteFile(good, []byte("asset"), 0o600))

	calls := 0
	p := &Provider{APIBaseOverride: "https://gitlab.invalid", HTTPClient: &http.Client{Transport: guardTransport(func(*http.Request) (*http.Response, error) {
		calls++

		return nil, errs.ErrDependencyUnavailable
	})}, Env: func(key string) string {
		if key == "GITLAB_TOKEN" {
			return "synthetic"
		}

		return ""
	}}

	for _, run := range []func(provider.ReleaseSpec) error{
		func(spec provider.ReleaseSpec) error { return p.CreateRelease(t.Context(), "owner/repo", spec) },
		func(spec provider.ReleaseSpec) error { return p.PublishRelease(t.Context(), "owner/repo", spec) },
	} {
		require.ErrorIs(t, run(provider.ReleaseSpec{Tag: "v1.2.3", Assets: []string{good, filepath.Join(root, "missing")}}), os.ErrNotExist)
	}

	require.Zero(t, calls)
}

func TestReleaseReplacement_UploadFailureKeepsOldLinks(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "asset")
	require.NoError(t, os.WriteFile(file, []byte("fixture"), 0o600))

	var calls []string

	p := &Provider{HTTPClient: &http.Client{Transport: guardTransport(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.Method+" "+req.URL.Path)
		if req.Body != nil {
			_, _ = io.Copy(io.Discard, req.Body)
		}

		return &http.Response{StatusCode: http.StatusForbidden, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("refused")), Request: req}, nil
	})}}
	err := p.uploadAndLinkReleaseAsset(t.Context(), "https://gitlab.invalid", "1", "owner/repo", "v1", file, map[string]string{})
	require.ErrorIs(t, err, errs.ErrPermissionDenied)
	require.Equal(t, []string{"POST /api/v4/projects/1/uploads"}, calls)
}
