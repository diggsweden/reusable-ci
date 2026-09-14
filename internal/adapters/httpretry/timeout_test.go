// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package httpretry_test

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/httpretry"
)

func TestParseClientTimeout_FallsBackToTheDefaultOnAnyUnusableValue(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]time.Duration{
		"":      httpretry.DefaultClientTimeout,
		"45s":   45 * time.Second,
		" 2m ":  2 * time.Minute,
		"soon":  httpretry.DefaultClientTimeout,
		"0s":    httpretry.DefaultClientTimeout,
		"-5s":   httpretry.DefaultClientTimeout,
		"   ":   httpretry.DefaultClientTimeout,
		"1500":  httpretry.DefaultClientTimeout,
		"250ms": 250 * time.Millisecond,
	} {
		if got := httpretry.ParseClientTimeout(raw); got != want {
			t.Errorf("ParseClientTimeout(%q) = %v, want %v", raw, got, want)
		}
	}
}

// TestClientTimeout_ReadsTheEnvironment is the one wiring check left on the
// process environment; the rules are tabled against the pure parser above.
func TestClientTimeout_ReadsTheEnvironment(t *testing.T) {
	// Not parallel: t.Setenv.
	t.Setenv("REUSABLE_CI_HTTP_TIMEOUT", "45s")

	if got := httpretry.ClientTimeout(); got != 45*time.Second {
		t.Errorf("ClientTimeout() = %v, want the 45s override", got)
	}
}

// TestClientTimeout_CapsTheWholeRetryLoop checks the claim on
// DefaultClientTimeout: the timeout is set on the http.Client, outside the
// retry transport, so it bounds every attempt and every wait together rather
// than each request. The server asks for a 3s pause that the transport would
// honour; the selected 50ms timeout must end the call during that pause, with
// no second attempt. Without the cap the call returns after 3s with the rate
// limit error instead, so the test fails rather than hangs.
func TestClientTimeout_CapsTheWholeRetryLoop(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	inner := serveInMemory(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	client := &http.Client{
		Timeout:   httpretry.ParseClientTimeout("50ms"),
		Transport: httpretry.NewTransport(httpretry.Config{Inner: inner, MaxAttempts: 2, MaxDelay: 5 * time.Second}),
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, testURL, nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want the client deadline to end the retry wait", err)
	}

	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1: the wait before the second was cut short", got)
	}
}
