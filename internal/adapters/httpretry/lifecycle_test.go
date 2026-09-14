// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package httpretry_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/httpretry"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// These tests replace a listener and a wall-clock sleep with an in-memory
// transport and a clock the test controls, so each outcome is exact rather
// than a race the scheduler usually wins.

// trackedBody records whether it was read to the end and closed. An
// intermediate response that is neither leaks its connection: the keep-alive
// pool cannot reuse it, and a retry loop against a flaky host opens a new one
// per attempt.
type trackedBody struct {
	io.Reader

	mu     sync.Mutex
	eof    bool
	closed bool
}

func (b *trackedBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	if errors.Is(err, io.EOF) {
		b.mu.Lock()
		b.eof = true
		b.mu.Unlock()
	}

	return n, err
}

func (b *trackedBody) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.closed = true

	return nil
}

func (b *trackedBody) drainedAndClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.eof && b.closed
}

// scripted answers each attempt with the next status and records the method
// and the body it handed out.
type scripted struct {
	mu       sync.Mutex
	statuses []int
	methods  []string
	bodies   []*trackedBody
}

func (s *scripted) RoundTrip(req *http.Request) (*http.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := s.statuses[len(s.methods)]
	body := &trackedBody{Reader: strings.NewReader(`{"attempt":"body"}`)}

	s.methods = append(s.methods, req.Method)
	s.bodies = append(s.bodies, body)

	return &http.Response{StatusCode: status, Body: body, Header: http.Header{}, Request: req}, nil
}

// cancellingClock cancels the request's context when the retry loop asks to
// wait, and returns a timer that fires only after a bound. The context is
// already done when the wait begins, so a loop that honours it leaves at once;
// a loop that ignores it waits out the bound and makes a second attempt, which
// the test reports as a failure instead of hanging until the package times out.
type cancellingClock struct {
	cancel context.CancelFunc
	waits  int
}

func (c *cancellingClock) Now() time.Time { return time.Time{} }

func (c *cancellingClock) NewTimer(time.Duration) *time.Timer {
	c.waits++
	c.cancel()

	return time.NewTimer(2 * time.Second)
}

// TestRoundTrip_CancellationDuringTheWaitStopsWithoutAnotherAttempt is the
// deterministic form of the cancellation guarantee, plus the half the existing
// test did not assert: that nothing is sent after the cancel.
//
// A retry that fires after its caller gave up is a request nobody is waiting
// for, made against a host that was already failing.
func TestRoundTrip_CancellationDuringTheWaitStopsWithoutAnotherAttempt(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	inner := &scripted{statuses: []int{http.StatusServiceUnavailable, http.StatusOK}}
	clock := &cancellingClock{cancel: cancel}

	transport := httpretry.NewTransport(httpretry.Config{
		MaxAttempts: 5, BaseDelay: time.Second, MaxDelay: time.Second, Inner: inner, Clock: clock,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.invalid/v1", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := transport.RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()

		t.Errorf("a cancelled retry returned a response: %d", resp.StatusCode)
	}

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}

	if got := len(inner.methods); got != 1 {
		t.Errorf("the transport made %d attempt(s); nothing may be sent after the cancel", got)
	}

	if clock.waits != 1 {
		t.Errorf("the loop waited %d time(s), want exactly the one it was cancelled in", clock.waits)
	}

	if !inner.bodies[0].drainedAndClosed() {
		t.Error("the response abandoned before the wait was not drained and closed")
	}
}

// TestRoundTrip_HEADIsRetriedLikeGET covers the second method retryableRequest
// admits, which no test sent. HEAD is how the registry clients probe for a
// manifest's existence, so a transient 503 on it deserves the same retry a GET
// gets.
func TestRoundTrip_HEADIsRetriedLikeGET(t *testing.T) {
	t.Parallel()

	inner := &scripted{statuses: []int{http.StatusServiceUnavailable, http.StatusOK}}

	transport := httpretry.NewTransport(httpretry.Config{
		MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, Inner: inner, Clock: newFakeClock(),
	})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodHead, "https://example.invalid/v2/app/manifests/v1", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}

	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want the retried 200", resp.StatusCode)
	}

	if want := []string{http.MethodHead, http.MethodHead}; strings.Join(inner.methods, ",") != strings.Join(want, ",") {
		t.Errorf("methods = %v, want %v", inner.methods, want)
	}
}

// TestRoundTrip_EveryDiscardedResponseIsDrainedAndClosed covers the body
// lifecycle across a retry run that ends in exhaustion.
//
// Two paths discard a response: each intermediate attempt that will be
// retried, and the final one when the loop gives up and returns an error
// instead. The RoundTripper contract forbids returning both a response and an
// error, so that last body is this function's to release, and nothing else
// will.
func TestRoundTrip_EveryDiscardedResponseIsDrainedAndClosed(t *testing.T) {
	t.Parallel()

	inner := &scripted{statuses: []int{
		http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusServiceUnavailable,
	}}

	transport := httpretry.NewTransport(httpretry.Config{
		MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, Inner: inner, Clock: newFakeClock(),
	})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.invalid/v1", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := transport.RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()

		t.Errorf("an exhausted retry returned a response alongside its error: %d", resp.StatusCode)
	}

	if !errors.Is(err, errs.ErrDependencyUnavailable) {
		t.Fatalf("err = %v, want ErrDependencyUnavailable", err)
	}

	if len(inner.bodies) != 3 {
		t.Fatalf("attempts = %d, want 3", len(inner.bodies))
	}

	for i, body := range inner.bodies {
		if !body.drainedAndClosed() {
			t.Errorf("response %d was not drained and closed", i+1)
		}
	}
}
