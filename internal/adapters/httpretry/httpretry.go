// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package httpretry wraps an http.RoundTripper with retry-on-transient-failure
// semantics: 502/503/504, 429 Too Many Requests (honouring Retry-After),
// and DNS/connection errors. Permanent failures (4xx other than 429,
// 2xx, 3xx) pass through unchanged.
//
// The transport is safe for use as a drop-in replacement for
// http.DefaultTransport. Use it via:
//
//	client := &http.Client{
//	    Timeout:   30 * time.Second,
//	    Transport: httpretry.NewTransport(httpretry.Config{}),
//	}
package httpretry

import (
	"context"
	"io"
	"log/slog"

	// math/rand/v2 is deliberate: this package's only random use is
	// retry-backoff jitter in backoff(); not security-sensitive.
	// crypto/rand would burn entropy for a non-cryptographic purpose.
	"math/rand/v2" // nosemgrep: go.lang.security.audit.crypto.math_random.math-random-used
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// Config tunes the retry behaviour. Zero values get safe defaults.
type Config struct {
	// MaxAttempts is the total number of request attempts including the
	// first. Zero → DefaultMaxAttempts.
	MaxAttempts int
	// BaseDelay is the initial backoff. Subsequent retries double until
	// MaxDelay. Zero → DefaultBaseDelay.
	BaseDelay time.Duration
	// MaxDelay caps the exponential backoff. Zero → DefaultMaxDelay.
	MaxDelay time.Duration
	// MaxCumulativeDelay caps total time spent sleeping across all
	// retries. Zero → DefaultMaxCumulativeDelay.
	MaxCumulativeDelay time.Duration
	// Inner is the transport actually doing the requests. Tests inject
	// an httptest-backed transport; production leaves nil → http.DefaultTransport.
	Inner http.RoundTripper
	// Clock is the time source used for sleep + Retry-After Date parsing.
	// Tests inject a fake; production leaves nil → real clock.
	Clock Clock
}

// Clock is the minimal interface the retry loop needs from a clock.
// Production wires the real clock; tests use a fake to advance virtual
// time without sleeping.
type Clock interface {
	Sleep(d time.Duration)
	Now() time.Time
}

// Defaults applied when Config fields are left zero.
const (
	DefaultMaxAttempts        = 4
	DefaultBaseDelay          = 500 * time.Millisecond
	DefaultMaxDelay           = 30 * time.Second
	DefaultMaxCumulativeDelay = 90 * time.Second
)

// realClock is the production Clock backed by stdlib time.
type realClock struct{}

func (realClock) Sleep(d time.Duration) { time.Sleep(d) }
func (realClock) Now() time.Time        { return time.Now() }

// Transport is the http.RoundTripper that applies the retry policy.
type Transport struct {
	cfg Config
}

// NewTransport returns a Transport with the given configuration. Zero
// values in cfg get production defaults.
func NewTransport(cfg Config) *Transport {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = DefaultMaxAttempts
	}

	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = DefaultBaseDelay
	}

	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = DefaultMaxDelay
	}

	if cfg.MaxCumulativeDelay <= 0 {
		cfg.MaxCumulativeDelay = DefaultMaxCumulativeDelay
	}

	if cfg.Inner == nil {
		cfg.Inner = http.DefaultTransport
	}

	if cfg.Clock == nil {
		cfg.Clock = realClock{}
	}

	return &Transport{cfg: cfg}
}

// RoundTrip implements http.RoundTripper.
//
// Methods that are not idempotent (POST without Idempotency-Key) are
// retried only on transport-level errors and 502/503/504 responses,
// where the server has stated it didn't accept the request. 429 is
// retried for every method — it explicitly invites a retry.
//
//nolint:cyclop // retry loop with one branch per backoff/abort/replay/result class.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	var (
		resp        *http.Response
		err         error
		cumulative  time.Duration
		retryReason string
	)

	for attempt := range t.cfg.MaxAttempts {
		// Rewind a request body on retries: net/http consumes the body
		// on the first send, so subsequent attempts need a fresh reader.
		if attempt > 0 && req.GetBody != nil {
			body, bodyErr := req.GetBody()
			if bodyErr != nil {
				return nil, bodyErr
			}

			req.Body = body
		}

		resp, err = t.cfg.Inner.RoundTrip(req)

		retryReason = retryDecision(resp, err)
		if retryReason == "" {
			return resp, err
		}

		// Drain + close the response body before retrying so the
		// connection can be reused.
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}

		if attempt == t.cfg.MaxAttempts-1 {
			break
		}

		delay := t.computeDelay(attempt, resp)
		if cumulative+delay > t.cfg.MaxCumulativeDelay {
			break
		}

		// Make the retry observable: a flaky-but-recovering downstream
		// is otherwise silent. Debug-gated, so it's noise-free normally
		// and visible under --log-level=debug.
		slog.Debug("http retry",
			"attempt", attempt+1, "max", t.cfg.MaxAttempts,
			"reason", retryReason, "delay", delay,
			"method", req.Method, "host", req.URL.Host)

		// Honour context cancellation while waiting.
		if !t.sleep(req.Context(), delay) {
			return nil, req.Context().Err()
		}

		cumulative += delay
	}

	if err != nil {
		return nil, err
	}
	// Permanent failure shape: surface a typed sentinel so callers can
	// distinguish "we retried, it's still not working" from a fresh 5xx.
	// Per the RoundTripper contract, when returning a non-nil error we
	// must not also return the response — drain + close it so the
	// connection can be reused.
	if resp != nil {
		switch resp.StatusCode {
		case http.StatusTooManyRequests:
			drainAndClose(resp)

			return nil, errs.ErrRateLimited
		case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			drainAndClose(resp)

			return nil, errs.ErrDependencyUnavailable
		}
	}

	return resp, nil
}

// drainAndClose consumes the body so the underlying connection can be
// returned to the keep-alive pool, then closes it. Errors are ignored —
// the caller is about to surface a different error.
func drainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// retryDecision returns a short non-empty reason when the response/error
// pair indicates a retryable failure, or "" when the result is final.
func retryDecision(resp *http.Response, err error) string {
	if err != nil {
		// net/http surfaces transport errors as plain errors; nothing
		// here is method-specific. We treat them all as retryable for
		// the GETs we make; POSTs without GetBody won't even reach this
		// point on retry, but we still try.
		return "transport: " + err.Error()
	}

	if resp == nil {
		return ""
	}

	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		return "429 Too Many Requests"
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		// Server is explicitly saying it didn't accept the request.
		// Safe to retry regardless of method.
		return strconv.Itoa(resp.StatusCode) + " " + resp.Status
	}

	return ""
}

// computeDelay picks the next sleep interval. It honours Retry-After
// (seconds form or HTTP-date form) when present and otherwise uses
// exponential backoff with full jitter.
func (t *Transport) computeDelay(attempt int, resp *http.Response) time.Duration {
	if d, ok := t.retryAfterDelay(resp); ok {
		return d
	}
	// Full jitter exponential backoff: rand within [0, 2^attempt * base].
	// Mitigates thundering-herd when many workflows retry simultaneously.
	upper := t.cfg.BaseDelay << uint(attempt) //nolint:gosec // small attempt bound
	if upper > t.cfg.MaxDelay {
		upper = t.cfg.MaxDelay
	}

	// nosemgrep: go.lang.security.audit.crypto.math_random.math-random-used
	// math/rand/v2 is deliberate: this is backoff jitter for retry pacing,
	// not security-sensitive randomness. crypto/rand here would burn entropy
	// for a non-cryptographic use.
	return time.Duration(rand.Int64N(int64(upper) + 1)) //nolint:gosec // backoff jitter — math/rand/v2 is fine here.
}

// retryAfterDelay returns the server-suggested delay parsed from the
// `Retry-After` response header — either the seconds form or the
// HTTP-date form. ok=false when no usable hint is present.
func (t *Transport) retryAfterDelay(resp *http.Response) (time.Duration, bool) {
	if resp == nil {
		return 0, false
	}

	h := strings.TrimSpace(resp.Header.Get("Retry-After")) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if h == "" {
		return 0, false
	}

	if secs, err := strconv.Atoi(h); err == nil && secs > 0 {
		return capDelay(time.Duration(secs)*time.Second, t.cfg.MaxDelay), true
	}

	if when, err := http.ParseTime(h); err == nil {
		if d := when.Sub(t.cfg.Clock.Now()); d > 0 {
			return capDelay(d, t.cfg.MaxDelay), true
		}
	}

	return 0, false
}

func capDelay(d, maxDelay time.Duration) time.Duration {
	if d > maxDelay {
		return maxDelay
	}

	return d
}

// sleep honours context cancellation. Returns false when the context
// expires during the sleep; true when the sleep ran to completion.
func (t *Transport) sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
