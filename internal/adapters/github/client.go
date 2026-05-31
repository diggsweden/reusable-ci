// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	gogithub "github.com/google/go-github/v76/github"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/httpretry"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// defaultHTTPClient returns a Client backed by the retry transport. The
// timeout caps total per-request wall time including retries (default
// 30s, overridable via REUSABLE_CI_HTTP_TIMEOUT for slow links); the
// transport's own MaxCumulativeDelay caps in-process sleep separately.
func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   httpretry.ClientTimeout(),
		Transport: httpretry.NewTransport(httpretry.Config{}),
	}
}

// httpClient returns the Provider's configured HTTP client (or the
// default retry-wrapped one). Bulk transfers that bypass go-github
// (artifact ZIP downloads, etc.) use this directly.
func (p *Provider) httpClient() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}

	return defaultHTTPClient()
}

// releaseClient returns the lazily-constructed go-github client. The
// client is configured with the retry transport, authenticates from
// GH_TOKEN / GITHUB_TOKEN, and honours APIBaseOverride so tests can
// point it at an httptest server.
//
// APIBaseOverride is set directly via BaseURL+UploadURL (not via
// WithEnterpriseURLs) — the latter forces the `/api/v3/` suffix that
// real GHE deployments use, which is wrong for our test fixtures and
// for the github.com path.
//
// Errors are sticky via sync.Once: once a malformed APIBaseOverride
// fails parsing, every method returns the same error rather than
// silently re-trying.
func (p *Provider) releaseClient(_ context.Context) (*gogithub.Client, error) {
	p.ghOnce.Do(func() {
		httpClient := p.HTTPClient
		if httpClient == nil {
			httpClient = defaultHTTPClient()
		}

		c := gogithub.NewClient(httpClient) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		get := p.envFunc()

		token := cmp.Or(get("GH_TOKEN"), get("GITHUB_TOKEN"))
		if token != "" {
			c = c.WithAuthToken(token)
		}

		base := strings.TrimSpace(p.APIBaseOverride)
		if base == "" {
			base = strings.TrimSpace(get("GITHUB_API_URL"))
		}

		if base != "" && base != defaultAPIBase {
			withSlash := strings.TrimRight(base, "/") + "/"

			u, err := url.Parse(withSlash) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
			if err != nil {
				p.ghErr = fmt.Errorf("configure GitHub API base %q: %w", base, err)

				return
			}
			// In production GHE deployments, BaseURL is api/v3/ and
			// UploadURL is api/uploads/. Tests collapse both onto one
			// httptest server, so point them at the same place; the
			// adapter's combinedHandler routes by path suffix.
			c.BaseURL = u
			c.UploadURL = u
		}

		p.ghClient = c
	})

	return p.ghClient, p.ghErr
}

// bearerHeader returns "Bearer <token>" or "" when token is empty.
func bearerHeader(token string) string {
	if token == "" {
		return ""
	}

	return "Bearer " + token
}

// classifyGitHubError maps go-github's *ErrorResponse onto our domain
// sentinels — typed status-code checks replace stderr-substring grep
// of the gh CLI.
func classifyGitHubError(err error) error {
	if err == nil {
		return nil
	}

	var resp *gogithub.ErrorResponse
	if errors.As(err, &resp) && resp.Response != nil {
		if cls := errs.FromHTTPStatus(resp.Response.StatusCode); cls != nil {
			return fmt.Errorf("%s: %w", resp.Message, cls)
		}

		if resp.Response.StatusCode == http.StatusNotFound {
			return errs.ErrReleaseNotFound
		}
	}

	return err
}

// getJSON does a GET with the provided headers, returns the body bytes.
// Empty Authorization is dropped. Caller is responsible for unmarshalling.
func getJSON(ctx context.Context, client *http.Client, url string, headers map[string]string) ([]byte, error) {
	if client == nil {
		client = defaultHTTPClient()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	for k, v := range headers {
		if v == "" {
			continue
		}

		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		cls := errs.FromHTTPStatus(resp.StatusCode)
		if cls == nil {
			cls = errs.ErrDependencyUnavailable
		}

		return nil, fmt.Errorf("HTTP %d: %s: %w", resp.StatusCode, string(body), cls)
	}

	return body, nil
}

// postJSON sends a POST request with the given body and headers,
// classifying non-2xx responses via errs.FromHTTPStatus when possible.
func postJSON(ctx context.Context, client *http.Client, url string, headers map[string]string, body []byte) error {
	if client == nil {
		client = defaultHTTPClient()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	for k, v := range headers {
		if v == "" {
			continue
		}

		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		cls := errs.FromHTTPStatus(resp.StatusCode)
		if cls == nil {
			cls = errs.ErrDependencyUnavailable
		}

		return fmt.Errorf("HTTP %d: %s: %w", resp.StatusCode, string(respBody), cls)
	}

	return nil
}

// gzipBase64 returns base64(gzip(in)) — the encoding the GitHub Code
// Scanning API requires for SARIF uploads.
func gzipBase64(in []byte) (string, error) {
	var gz bytes.Buffer

	w := gzip.NewWriter(&gz)
	if _, err := w.Write(in); err != nil {
		return "", err
	}

	if err := w.Close(); err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(gz.Bytes()), nil
}
