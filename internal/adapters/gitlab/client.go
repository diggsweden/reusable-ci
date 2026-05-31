// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/httpretry"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// defaultHTTPClient returns a Client backed by the retry transport. The
// timeout caps total per-request wall time (default 30s, overridable via
// REUSABLE_CI_HTTP_TIMEOUT for slow links); the transport's
// MaxCumulativeDelay caps in-process sleep separately.
func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   httpretry.ClientTimeout(),
		Transport: httpretry.NewTransport(httpretry.Config{}),
	}
}

// getJSON does a GET with the provided headers, returns the body bytes.
// Empty PRIVATE-TOKEN is dropped. Caller is responsible for
// unmarshalling.
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
		return err
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
