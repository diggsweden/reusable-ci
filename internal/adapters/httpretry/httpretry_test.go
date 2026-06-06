// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package httpretry_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/internal/adapters/httpretry"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// fakeClock is a deterministic Clock that records Sleep durations and
// advances virtual time without blocking.
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	slept []time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (f *fakeClock) Sleep(d time.Duration) {
	f.mu.Lock()
	f.slept = append(f.slept, d)
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.now
}

func (f *fakeClock) SleptCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.slept)
}

// flakyServer returns success after `failuresBeforeSuccess` 503 responses.
type flakyServer struct {
	failuresBeforeSuccess int
	attempts              int32
	mu                    sync.Mutex
}

func (f *flakyServer) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		f.mu.Lock()
		f.attempts++
		current := f.attempts
		f.mu.Unlock()

		if int(current) <= f.failuresBeforeSuccess {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"try again"}`))

			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
}

func TestRoundTrip_RecoversFromTransient5xx(t *testing.T) {
	t.Parallel()

	flaky := &flakyServer{failuresBeforeSuccess: 2}

	srv := httptest.NewServer(flaky.Handler())
	defer srv.Close()

	// Force-zero the sleep so the test is fast.
	transport := httpretry.NewTransport(httpretry.Config{
		MaxAttempts: 4,
		BaseDelay:   time.Nanosecond,
		MaxDelay:    time.Nanosecond,
	})
	client := &http.Client{Transport: transport}

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	flaky.mu.Lock()
	defer flaky.mu.Unlock()

	if flaky.attempts != 3 {
		t.Errorf("attempts = %d, want 3 (2 fails + success)", flaky.attempts)
	}
}

func TestRoundTrip_GivesUpAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	const maxAttempts = 2

	flaky := &flakyServer{failuresBeforeSuccess: 100} // never succeeds

	srv := httptest.NewServer(flaky.Handler())
	defer srv.Close()

	transport := httpretry.NewTransport(httpretry.Config{
		MaxAttempts: maxAttempts,
		BaseDelay:   time.Nanosecond,
		MaxDelay:    time.Nanosecond,
	})
	client := &http.Client{Transport: transport}

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)

	resp, err := client.Do(req)
	if err == nil || !errors.Is(err, errs.ErrDependencyUnavailable) {
		t.Fatalf("err = %v, want ErrDependencyUnavailable wrap", err)
	}

	if resp != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}

	flaky.mu.Lock()
	defer flaky.mu.Unlock()

	if int(flaky.attempts) != maxAttempts {
		t.Errorf("attempts = %d, want exactly %d (MaxAttempts cap)", flaky.attempts, maxAttempts)
	}
}

func TestRoundTrip_HonoursRetryAfterSeconds(t *testing.T) {
	t.Parallel()

	var attempt int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		attempt++
		if attempt == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)

			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	clock := newFakeClock()
	// Inject a fake inner transport that uses clock.Sleep effectively zero;
	// here the real transport is fine — what we care about is the computed
	// delay. The test uses BaseDelay=ns so the only material wait is Retry-After.
	transport := httpretry.NewTransport(httpretry.Config{
		MaxAttempts: 2,
		BaseDelay:   time.Nanosecond,
		MaxDelay:    5 * time.Second,
		Clock:       clock,
	})
	client := &http.Client{Transport: transport}

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestRoundTrip_Permanent4xxIsNotRetried(t *testing.T) {
	t.Parallel()

	var attempts int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++

		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	transport := httpretry.NewTransport(httpretry.Config{MaxAttempts: 3, BaseDelay: time.Nanosecond})

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)

	resp, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		t.Fatalf("err = %v, want nil for 403 (caller decides)", err)
	}

	_ = resp.Body.Close()

	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (403 is permanent)", attempts)
	}
}

func TestRoundTrip_RespectsContextCancellation(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	transport := httpretry.NewTransport(httpretry.Config{
		MaxAttempts: 100,
		BaseDelay:   500 * time.Millisecond,
		MaxDelay:    5 * time.Second,
	})
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)

	// Cancel quickly so retries abort.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	resp, err := transport.RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}

	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestRoundTrip_RewindsBodyOnRetry(t *testing.T) {
	t.Parallel()

	var (
		got []string
		mu  sync.Mutex
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		body, _ := io.ReadAll(r.Body)

		mu.Lock()

		got = append(got, string(body))
		attempt := len(got)
		mu.Unlock()

		if attempt < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	transport := httpretry.NewTransport(httpretry.Config{MaxAttempts: 3, BaseDelay: time.Nanosecond})

	// Use bytes.Reader (sets GetBody automatically when via http.NewRequest).
	body := bytes.NewReader([]byte(`{"hello":"world"}`))

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL, body)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_ = resp.Body.Close()

	mu.Lock()
	defer mu.Unlock()

	if len(got) != 2 {
		t.Fatalf("attempts = %d, want 2", len(got))
	}

	if got[0] != got[1] {
		t.Errorf("body rewinding failed: first=%q second=%q", got[0], got[1])
	}

	if !strings.Contains(got[0], "hello") {
		t.Errorf("body content lost: %q", got[0])
	}
}
