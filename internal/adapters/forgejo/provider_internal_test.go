// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo

import "testing"

// TestHTTPClientDefaultIsBounded guards the production path: when no
// client is injected (forgejo.New()), the Gitea SDK must NOT fall back
// to its unbounded &http.Client{} default. An unbounded client lets a
// slow/hung Forgejo server stall a release step forever, so the timeout
// is a hard requirement, not a nicety.
func TestHTTPClientDefaultIsBounded(t *testing.T) {
	t.Parallel()

	c := New().httpClient()
	if c.Timeout <= 0 {
		t.Fatalf("default forgejo client has no timeout (got %v): a hung server would stall forever", c.Timeout)
	}

	if c.Transport == nil {
		t.Error("default forgejo client is missing the retry transport")
	}
}

// TestHTTPClientHonorsInjection keeps the test seam working: an injected
// client (httptest-backed in tests) is used verbatim, not replaced.
func TestHTTPClientHonorsInjection(t *testing.T) {
	t.Parallel()

	injected := defaultHTTPClient()
	p := &Provider{HTTPClient: injected}

	if p.httpClient() != injected {
		t.Error("injected HTTPClient was not used")
	}
}
