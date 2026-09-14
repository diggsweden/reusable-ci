// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package httpretry wraps an http.RoundTripper with retry-on-transient-failure
// semantics for safe reads: GET and HEAD requests may retry 502/503/504, 429
// Too Many Requests (honouring Retry-After), and DNS/connection errors.
// Mutations are sent once because these failures do not prove the server did
// not commit the request.
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
	"fmt"
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
	"math/bits"
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
	// Clock is the time source used for the retry wait and for
	// Retry-After Date parsing.
	// Tests inject a fake; production leaves nil → real clock.
	Clock Clock
}

// Clock is the minimal interface the retry loop needs from a clock.
// Production wires the real clock; tests use a fake to make the waits
// instant while still observing the durations that were asked for.
//
// NewTimer rather than a blocking Sleep(d): the retry wait must lose a
// race against context cancellation (see Transport.sleep), and a
// blocking sleep cannot be interrupted. Returning a *time.Timer keeps
// Stop() available, so an abandoned wait releases its timer instead of
// leaving one armed for up to MaxDelay.
type Clock interface {
	Now() time.Time
	NewTimer(d time.Duration) *time.Timer
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

func (realClock) Now() time.Time                       { return time.Now() }
func (realClock) NewTimer(d time.Duration) *time.Timer { return time.NewTimer(d) }

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
// Only GET and HEAD are retried. A request with a body is retryable only when
// net/http can recreate that body through GetBody. Mutations require
// operation-specific idempotency or reconciliation and therefore pass through
// to the inner transport exactly once.
//
//nolint:cyclop // retry loop with one branch per backoff/abort/replay/result class.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !retryableRequest(req) {
		resp, err := t.cfg.Inner.RoundTrip(req)

		return resp, safeTransportError(err)
	}

	var (
		resp        *http.Response
		err         error
		cumulative  time.Duration
		retryReason string
	)

	for attempt := range t.cfg.MaxAttempts {
		// Rewind a request body on retries: net/http consumes the body
		// on the first send, so subsequent attempts need a fresh reader.
		if attempt > 0 && req.Body != nil && req.Body != http.NoBody {
			body, bodyErr := req.GetBody()
			if bodyErr != nil {
				return nil, safeTransportError(bodyErr)
			}

			req.Body = body
		}

		resp, err = t.cfg.Inner.RoundTrip(req)

		retryReason = retryDecision(resp, err)
		if retryReason == "" {
			return resp, safeTransportError(err)
		}

		if attempt == t.cfg.MaxAttempts-1 {
			break
		}

		delay := t.computeDelay(attempt, resp)
		if cumulative+delay > t.cfg.MaxCumulativeDelay {
			break
		}

		// Drain + close only when another attempt will be made. The terminal
		// response is consumed once by the exhaustion path below.
		if resp != nil {
			drainAndClose(resp)
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
		return nil, safeTransportError(err)
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

			return nil, exhaustedError(req, resp, errs.ErrRateLimited)
		case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			drainAndClose(resp)

			return nil, exhaustedError(req, resp, errs.ErrDependencyUnavailable)
		}
	}

	return resp, nil
}

// exhaustedError names the request and terminal status behind a retry
// sentinel, so "rate limited" reaches the operator with what was throttled
// rather than as a bare class. Only the method and host are public diagnostics.
func exhaustedError(req *http.Request, resp *http.Response, sentinel error) error {
	return fmt.Errorf("%s %s: HTTP %d, retries exhausted: %w", req.Method, req.URL.Host, resp.StatusCode, sentinel)
}

type transportError struct{ cause error }

func (e transportError) Error() string { return "HTTP transport failed" }
func (e transportError) Unwrap() error { return e.cause }

func safeTransportError(err error) error {
	if err == nil {
		return nil
	}

	return transportError{cause: err}
}

func retryableRequest(req *http.Request) bool {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return false
	}

	return req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
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
		// RoundTrip reaches this decision only for replayable GET/HEAD
		// requests; mutations bypass the retry loop entirely.
		return "transport failure"
	}

	if resp == nil {
		return ""
	}

	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		return "429 Too Many Requests"
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return strconv.Itoa(resp.StatusCode) + " " + http.StatusText(resp.StatusCode)
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
	//
	// The shift saturates instead of wrapping. Doubling a Duration is a shift
	// on an int64, and it used to be taken unguarded: with a one-second base
	// the product passes MaxInt64 at attempt 34 and comes back NEGATIVE, so
	// the cap below (a negative value is not greater than MaxDelay) left it
	// negative and rand.Int64N panicked on a non-positive bound — a crash
	// inside a retry, which is the one place a caller has asked not to fail.
	// Past attempt 62 the shift reaches zero instead, and a zero backoff is a
	// hot loop against whatever was already refusing the request.
	//
	// Neither is reachable with the retry counts this package ships, and both
	// are reachable from a Config an adopter supplies, which is why the bound
	// is enforced here rather than assumed at the call site.
	upper := t.cfg.MaxDelay
	if base := t.cfg.BaseDelay; base > 0 && attempt >= 0 && attempt < bits.LeadingZeros64(uint64(base)) {
		if shifted := base << uint(attempt); shifted < upper { //nolint:gosec // bounded by the guard above.
			upper = shifted
		}
	}

	if upper <= 0 {
		return 0
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

	if strings.Trim(h, "0123456789") == "" {
		secs, err := strconv.ParseInt(h, 10, 64)
		if err != nil || secs > int64(t.cfg.MaxDelay/time.Second) {
			return t.cfg.MaxDelay, true
		}

		return time.Duration(secs) * time.Second, true
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
	timer := t.cfg.Clock.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
