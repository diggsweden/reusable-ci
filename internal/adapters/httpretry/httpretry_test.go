// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package httpretry_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/httpretry"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
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

// NewTimer records the requested wait, advances virtual time by it, and
// returns a timer that fires at once — so a test observes the delay the
// retry loop asked for without spending it.
func (f *fakeClock) NewTimer(d time.Duration) *time.Timer {
	f.mu.Lock()
	f.slept = append(f.slept, d)
	f.now = f.now.Add(d)
	f.mu.Unlock()

	return time.NewTimer(0)
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.now
}

// Waited returns a copy of the recorded retry waits, in order.
func (f *fakeClock) Waited() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.slept)
}

// flakyServer returns success after `failuresBeforeSuccess` 503 responses.
type flakyServer struct {
	failuresBeforeSuccess int
	attempts              int32
	mu                    sync.Mutex
}

// testURL is where every in-memory request goes. Nothing resolves it: the
// transport under test is handed a handler directly, not a network.
const testURL = "http://api.example.invalid/resource"

// serveInMemory answers requests with handler through a response recorder, so
// the retry loop sees real *http.Response values without a listener. Like any
// RoundTripper it closes the request body once the handler is done with it.
func serveInMemory(handler http.HandlerFunc) http.RoundTripper {
	return roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Body != nil {
			defer func() { _ = request.Body.Close() }()
		}

		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		response := recorder.Result()
		response.Request = request

		return response, nil
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type transportTestError struct{}

func (*transportTestError) Error() string { return "ambiguous write result" }

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

	inner := serveInMemory(flaky.Handler())

	// Force-zero the sleep so the test is fast.
	transport := httpretry.NewTransport(httpretry.Config{
		Inner:       inner,
		MaxAttempts: 4,
		BaseDelay:   time.Nanosecond,
		MaxDelay:    time.Nanosecond,
	})
	client := &http.Client{Transport: transport}

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, testURL, nil)

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

	inner := serveInMemory(flaky.Handler())

	transport := httpretry.NewTransport(httpretry.Config{
		Inner:       inner,
		MaxAttempts: maxAttempts,
		BaseDelay:   time.Nanosecond,
		MaxDelay:    time.Nanosecond,
	})
	client := &http.Client{Transport: transport}

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, testURL, nil)

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

// TestRoundTrip_HonoursRetryAfter pins the delay the server asked for.
// The 200 at the end is not the claim: with BaseDelay=1ns a transport
// that ignored Retry-After entirely would also recover on the second
// attempt and look identical. Only the recorded wait separates "paused
// as instructed" from "hammered the server".
func TestRoundTrip_HonoursRetryAfter(t *testing.T) {
	t.Parallel()

	clockBase := newFakeClock().Now()

	for _, testCase := range []struct {
		name       string
		retryAfter func() string
		want       time.Duration
	}{
		{
			name:       "seconds form",
			retryAfter: func() string { return "2" },
			want:       2 * time.Second,
		},
		{
			// The HTTP-date form is why Clock.Now exists: the delay is the
			// difference against the clock's own present.
			name:       "http-date form",
			retryAfter: func() string { return clockBase.Add(3 * time.Second).Format(http.TimeFormat) },
			want:       3 * time.Second,
		},
		{
			// Above MaxDelay, so the cap decides. A server naming an hour
			// must not park a release job for an hour.
			name:       "capped at MaxDelay",
			retryAfter: func() string { return "3600" },
			want:       5 * time.Second,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var (
				mu      sync.Mutex
				attempt int
			)

			inner := serveInMemory(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				mu.Lock()
				attempt++
				first := attempt == 1
				mu.Unlock()

				if first {
					w.Header().Set("Retry-After", testCase.retryAfter())
					w.WriteHeader(http.StatusTooManyRequests)

					return
				}

				w.WriteHeader(http.StatusOK)
			}))

			clock := newFakeClock()
			transport := httpretry.NewTransport(httpretry.Config{
				Inner:       inner,
				MaxAttempts: 2,
				BaseDelay:   time.Nanosecond,
				MaxDelay:    5 * time.Second,
				Clock:       clock,
			})

			req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, testURL, nil)

			resp, err := (&http.Client{Transport: transport}).Do(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}

			waited := clock.Waited()
			if len(waited) != 1 {
				t.Fatalf("waits = %v, want exactly one retry wait", waited)
			}

			if waited[0] != testCase.want {
				t.Errorf("waited %v, want %v (the delay the server asked for)", waited[0], testCase.want)
			}
		})
	}
}

// TestRoundTrip_StopsWhenTheCumulativeDelayBudgetWouldBeExceeded covers
// MaxCumulativeDelay, which had no test at all: it is the guard that
// stops a downstream naming a long Retry-After, over and over, from
// parking a release job for the whole of MaxAttempts.
//
// The budget refuses the next wait rather than shortening it — a
// truncated wait would go back to the server earlier than it asked.
func TestRoundTrip_StopsWhenTheCumulativeDelayBudgetWouldBeExceeded(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		attempts int
	)

	inner := serveInMemory(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		attempts++
		mu.Unlock()

		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
	}))

	clock := newFakeClock()
	transport := httpretry.NewTransport(httpretry.Config{
		Inner: inner,
		// Room for far more attempts than the budget allows, so the
		// budget is what stops the loop and not MaxAttempts.
		MaxAttempts:        10,
		BaseDelay:          time.Nanosecond,
		MaxDelay:           5 * time.Second,
		MaxCumulativeDelay: 7 * time.Second,
		Clock:              clock,
	})

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, testURL, nil)

	resp, err := (&http.Client{Transport: transport}).Do(req)
	if resp != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}

	// ErrRateLimited, not the generic unavailable class: the terminal 429
	// is classified on its own status, so a caller can still tell "the
	// forge throttled us" from "the forge was down".
	if !errors.Is(err, errs.ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited once the budget is spent", err)
	}

	// The sentinel alone told an operator nothing about what was throttled.
	if !strings.Contains(err.Error(), "HTTP 429") || !strings.Contains(err.Error(), testURL) {
		t.Errorf("err = %v, want it to name the throttled request and status", err)
	}

	// Two waits of 3s fit; the third would reach 9s and is refused.
	if waited := clock.Waited(); !slices.Equal(waited, []time.Duration{3 * time.Second, 3 * time.Second}) {
		t.Errorf("waits = %v, want two 3s waits before the 7s budget refuses a third", waited)
	}

	mu.Lock()
	defer mu.Unlock()

	if attempts != 3 {
		t.Errorf("attempts = %d, want 3 (two waits, then give up well short of MaxAttempts)", attempts)
	}
}

func TestRoundTrip_Permanent4xxIsNotRetried(t *testing.T) {
	t.Parallel()

	var attempts int32

	inner := serveInMemory(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++

		w.WriteHeader(http.StatusForbidden)
	}))

	transport := httpretry.NewTransport(httpretry.Config{Inner: inner, MaxAttempts: 3, BaseDelay: time.Nanosecond})

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, testURL, nil)

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

	inner := serveInMemory(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))

	transport := httpretry.NewTransport(httpretry.Config{
		Inner:       inner,
		MaxAttempts: 100,
		BaseDelay:   500 * time.Millisecond,
		MaxDelay:    5 * time.Second,
	})
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)

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

func TestRoundTrip_RewindsReplayableReadBody(t *testing.T) {
	t.Parallel()

	var (
		got []string
		mu  sync.Mutex
	)

	inner := serveInMemory(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
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

	transport := httpretry.NewTransport(httpretry.Config{Inner: inner, MaxAttempts: 3, BaseDelay: time.Nanosecond})

	// Use bytes.Reader (sets GetBody automatically when via http.NewRequest).
	body := bytes.NewReader([]byte(`{"hello":"world"}`))

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, testURL, body)
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

func TestRoundTrip_DoesNotRetryMutations(t *testing.T) {
	t.Parallel()

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		for _, status := range []int{http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
			t.Run(method+"_"+strconv.Itoa(status), func(t *testing.T) {
				t.Parallel()

				attempts := 0
				wantResp := &http.Response{
					StatusCode: status,
					Status:     strconv.Itoa(status) + " " + http.StatusText(status),
					Body:       io.NopCloser(strings.NewReader("original mutation response")),
				}
				transport := httpretry.NewTransport(httpretry.Config{
					MaxAttempts: 3,
					Inner: roundTripFunc(func(*http.Request) (*http.Response, error) {
						attempts++

						return wantResp, nil
					}),
				})

				req, err := http.NewRequestWithContext(t.Context(), method, "https://example.invalid/resource", bytes.NewReader([]byte("replayable but unsafe")))
				if err != nil {
					t.Fatal(err)
				}

				resp, err := transport.RoundTrip(req)
				if err != nil {
					t.Fatalf("mutation response should pass through unchanged: %v", err)
				}

				if resp != wantResp {
					t.Fatalf("response pointer changed: got %p want %p", resp, wantResp)
				}

				body, readErr := io.ReadAll(resp.Body)
				_ = resp.Body.Close()

				if readErr != nil {
					t.Fatal(readErr)
				}

				if string(body) != "original mutation response" {
					t.Fatalf("response body changed or drained: %q", body)
				}

				if attempts != 1 {
					t.Fatalf("%s attempts = %d, want 1", method, attempts)
				}
			})
		}
	}
}

func TestRoundTrip_DoesNotRetryMutationTransportError(t *testing.T) {
	t.Parallel()

	wantErr := &transportTestError{}
	attempts := 0
	transport := httpretry.NewTransport(httpretry.Config{
		MaxAttempts: 3,
		Inner: roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts++

			return nil, wantErr
		}),
	})

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://example.invalid/resource", bytes.NewReader([]byte("body")))
	if err != nil {
		t.Fatal(err)
	}

	resp, err := transport.RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}

	var gotErr *transportTestError
	if !errors.As(err, &gotErr) || gotErr != wantErr {
		t.Fatalf("error = %v, want original transport error", err)
	}

	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRoundTrip_DoesNotRetryNonReplayableReadBody(t *testing.T) {
	t.Parallel()

	attempts := 0
	wantResp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Status:     "503 Service Unavailable",
		Body:       io.NopCloser(strings.NewReader("original read response")),
	}
	transport := httpretry.NewTransport(httpretry.Config{
		MaxAttempts: 3,
		Inner: roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts++

			return wantResp, nil
		}),
	})

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.invalid/resource", nil)
	if err != nil {
		t.Fatal(err)
	}

	req.Body = io.NopCloser(strings.NewReader("cannot rewind"))
	req.GetBody = nil

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("non-replayable response should pass through unchanged: %v", err)
	}

	if resp != wantResp {
		t.Fatalf("response pointer changed: got %p want %p", resp, wantResp)
	}

	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if readErr != nil {
		t.Fatal(readErr)
	}

	if string(body) != "original read response" {
		t.Fatalf("response body changed or drained: %q", body)
	}

	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRoundTrip_StopsWhenGetBodyFails(t *testing.T) {
	t.Parallel()

	wantErr := errs.ErrDependencyUnavailable
	attempts := 0
	transport := httpretry.NewTransport(httpretry.Config{
		MaxAttempts: 3,
		BaseDelay:   time.Nanosecond,
		MaxDelay:    time.Nanosecond,
		Inner: roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts++

			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Status:     "503 Service Unavailable",
				Body:       io.NopCloser(strings.NewReader("try again")),
			}, nil
		}),
	})

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.invalid/resource", bytes.NewReader([]byte("body")))
	if err != nil {
		t.Fatal(err)
	}

	req.GetBody = func() (io.ReadCloser, error) { return nil, wantErr }

	resp, err := transport.RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}

	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want GetBody error", err)
	}

	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

// TestRoundTrip_LogsRetryAtDebug asserts retries are observable: a
// flaky-but-recovering downstream emits a debug log per retry (reason +
// status), so an operator running --log-level=debug can see retry counts.
func TestRoundTrip_LogsRetryAtDebug(t *testing.T) {
	// Swaps the global slog default, so no t.Parallel().
	var logBuf bytes.Buffer

	prev := slog.Default()

	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	flaky := &flakyServer{failuresBeforeSuccess: 1}

	inner := serveInMemory(flaky.Handler())

	transport := httpretry.NewTransport(httpretry.Config{
		Inner:       inner,
		MaxAttempts: 3,
		BaseDelay:   time.Nanosecond,
		MaxDelay:    time.Nanosecond,
	})
	client := &http.Client{Transport: transport}

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, testURL, nil)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_ = resp.Body.Close()

	out := logBuf.String()
	if !strings.Contains(out, "http retry") {
		t.Errorf("expected a 'http retry' debug log; got: %q", out)
	}

	if !strings.Contains(out, "reason=") || !strings.Contains(out, "503") {
		t.Errorf("retry log missing reason/status detail: %q", out)
	}
}
