// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

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

// doRequest applies the same authority boundary to injected and default clients
// without modifying the caller's client or redirect policy.
func doRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	if client == nil {
		client = defaultHTTPClient()
	}

	guarded := *client
	previous := client.CheckRedirect
	guarded.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("GitLab redirect limit reached: %w", errs.ErrDependencyUnavailable)
		}

		if previous != nil {
			if err := previous(next, via); err != nil {
				return err
			}
		}

		if next.URL.User != nil || !strings.EqualFold(next.URL.Scheme, req.URL.Scheme) || !strings.EqualFold(next.URL.Host, req.URL.Host) {
			return fmt.Errorf("GitLab redirect changes authority: %w", errs.ErrValidation)
		}

		return nil
	}
	resp, err := guarded.Do(req)

	return resp, privateRequestError(err)
}

type requestError struct{ cause error }

func (e requestError) Error() string { return "GitLab request failed" }
func (e requestError) Unwrap() error { return e.cause }
func privateRequestError(err error) error {
	if err == nil {
		return nil
	}

	return requestError{cause: err}
}

func responseStatusError(status int) error {
	cls := errs.FromHTTPStatus(status)
	if cls == nil {
		cls = errs.ErrDependencyUnavailable
	}

	return fmt.Errorf("HTTP %d: %w", status, cls)
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
		return nil, privateRequestError(err)
	}

	for k, v := range headers {
		if v == "" {
			continue
		}

		req.Header.Set(k, v)
	}

	resp, err := doRequest(client, req)
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, privateRequestError(err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, responseStatusError(resp.StatusCode)
	}

	return body, nil
}

// postJSON sends a POST request with the given body and headers,
// classifying non-2xx responses via errs.FromHTTPStatus when possible.
func postJSON(ctx context.Context, client *http.Client, url string, headers map[string]string, body []byte) error {
	return sendJSON(ctx, client, http.MethodPost, url, headers, body)
}

// putJSON is the in-place update (PUT) counterpart of postJSON, used by the
// reconcile publish strategy.
func putJSON(ctx context.Context, client *http.Client, url string, headers map[string]string, body []byte) error {
	return sendJSON(ctx, client, http.MethodPut, url, headers, body)
}

// sendJSON performs a body-carrying request (POST/PUT) with the given headers,
// classifying non-2xx responses via errs.FromHTTPStatus when possible.
func sendJSON(ctx context.Context, client *http.Client, method, url string, headers map[string]string, body []byte) error {
	if client == nil {
		client = defaultHTTPClient()
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return privateRequestError(err)
	}

	for k, v := range headers {
		if v == "" {
			continue
		}

		req.Header.Set(k, v)
	}

	resp, err := doRequest(client, req)
	if err != nil {
		return err
	}

	defer func() { _ = resp.Body.Close() }()

	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseStatusError(resp.StatusCode)
	}

	return nil
}

// deleteJSON sends a DELETE request, classifying non-2xx responses via
// errs.FromHTTPStatus when possible.
func deleteJSON(ctx context.Context, client *http.Client, url string, headers map[string]string) error {
	if client == nil {
		client = defaultHTTPClient()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return privateRequestError(err)
	}

	for k, v := range headers {
		if v == "" {
			continue
		}

		req.Header.Set(k, v)
	}

	resp, err := doRequest(client, req)
	if err != nil {
		return err
	}

	defer func() { _ = resp.Body.Close() }()

	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseStatusError(resp.StatusCode)
	}

	return nil
}
