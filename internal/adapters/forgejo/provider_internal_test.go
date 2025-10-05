// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo

import (
	"net/http"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/httpretry"
)

// TestHTTPClient_DefaultIsBounded guards the production path: when no
// client is injected (forgejo.New()), the Gitea SDK must NOT fall back
// to its unbounded &http.Client{} default. An unbounded client lets a
// slow/hung Forgejo server stall a release step forever, so the timeout
// is a hard requirement, not a nicety.
func TestHTTPClient_DefaultIsBounded(t *testing.T) {
	t.Parallel()

	c := New().httpClient()
	if c.Timeout <= 0 {
		t.Fatalf("default forgejo client has no timeout (got %v): a hung server would stall forever", c.Timeout)
	}

	// The timeout is the shared one, compared against the same process
	// environment the client read, so REUSABLE_CI_HTTP_TIMEOUT being set or
	// not on the machine running the suite cannot change the outcome.
	if want := httpretry.ClientTimeout(); c.Timeout != want {
		t.Errorf("default forgejo client timeout = %v, want the shared httpretry timeout %v", c.Timeout, want)
	}

	// Not merely a non-nil transport: http.DefaultTransport is non-nil too,
	// and a client built on it would lose every retry and backoff bound the
	// github and gitlab adapters apply.
	if _, ok := c.Transport.(*httpretry.Transport); !ok {
		t.Errorf("default forgejo client transport = %T, want *httpretry.Transport", c.Transport)
	}
}

// TestHTTPClient_HonorsInjection keeps the test seam working: an injected
// client (httptest-backed in tests) is used verbatim, not replaced.
func TestHTTPClient_HonorsInjection(t *testing.T) {
	t.Parallel()

	// A client that shares nothing with the default, so using the default
	// instead cannot look like honouring the injection.
	injected := &http.Client{Timeout: time.Minute}
	p := &Provider{HTTPClient: injected}

	if p.httpClient() != injected {
		t.Error("injected HTTPClient was not used")
	}
}
