// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package retry_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/retry"
)

var errBoom = errors.New("boom")

func TestDo_ReturnsFirstSuccessWithoutRetrying(t *testing.T) {
	t.Parallel()

	calls := 0

	got, err := retry.Do(context.Background(), io.Discard, 3, time.Nanosecond, func() (string, error) {
		calls++

		return "ok", nil
	})
	if err != nil || got != "ok" {
		t.Fatalf("got %q, %v", got, err)
	}

	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestDo_RetriesThenSucceedsAndLogs(t *testing.T) {
	t.Parallel()

	calls := 0

	var out strings.Builder

	got, err := retry.Do(context.Background(), &out, 3, time.Nanosecond, func() (int, error) {
		calls++
		if calls < 3 {
			return 0, errBoom
		}

		return calls, nil
	})
	if err != nil || got != 3 || calls != 3 {
		t.Fatalf("got %d, %v (calls=%d)", got, err, calls)
	}

	if !strings.Contains(out.String(), "attempt 1/3") || !strings.Contains(out.String(), "attempt 2/3") {
		t.Fatalf("progress log missing: %q", out.String())
	}
}

func TestDo_ExhaustsAndReturnsLastErrorAndZero(t *testing.T) {
	t.Parallel()

	causes := []error{errors.New("first attempt"), errors.New("middle attempt"), errors.New("last attempt")} //nolint:err113 // independent attempt sentinels must remain distinguishable.
	calls := 0

	got, err := retry.Do(t.Context(), io.Discard, 3, 0, func() (string, error) {
		cause := causes[min(calls, len(causes)-1)]
		calls++

		return "partial", cause
	})
	if calls != 3 || !errors.Is(err, causes[2]) {
		t.Fatalf("calls=%d err=%v, want three attempts and last cause", calls, err)
	}

	if got != "" {
		t.Fatalf("got = %q, want zero value on failure", got)
	}
}

func TestDo_AbortsOnContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := 0

	_, err := retry.Do(ctx, io.Discard, 5, time.Hour, func() (struct{}, error) {
		calls++

		return struct{}{}, errBoom
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}

	if calls != 1 {
		t.Fatalf("calls = %d, want 1 before honoring cancellation", calls)
	}
}

func TestDo_LinearBackoffGrowsWaitPerAttempt(t *testing.T) {
	t.Parallel()

	var waits []time.Duration

	_, _ = retry.Do(context.Background(), io.Discard, 3, 5*time.Nanosecond, func() (struct{}, error) {
		return struct{}{}, errBoom
	}, retry.OnRetry(func(_, _ int, wait time.Duration, _ error) {
		waits = append(waits, wait)
	}))

	want := []time.Duration{5 * time.Nanosecond, 10 * time.Nanosecond}
	if len(waits) != len(want) || waits[0] != want[0] || waits[1] != want[1] {
		t.Fatalf("linear waits = %v, want %v", waits, want)
	}
}

func TestDo_ConstantBackoffKeepsWaitFixed(t *testing.T) {
	t.Parallel()

	var waits []time.Duration

	_, _ = retry.Do(context.Background(), io.Discard, 3, 5*time.Nanosecond, func() (struct{}, error) {
		return struct{}{}, errBoom
	}, retry.WithBackoff(retry.Constant), retry.OnRetry(func(_, _ int, wait time.Duration, _ error) {
		waits = append(waits, wait)
	}))

	for i, w := range waits {
		if w != 5*time.Nanosecond {
			t.Fatalf("constant wait[%d] = %s, want 5ns", i, w)
		}
	}

	if len(waits) != 2 {
		t.Fatalf("expected 2 retries logged, got %d", len(waits))
	}
}

func TestDo_PermanentStopsImmediatelyAndUnwraps(t *testing.T) {
	t.Parallel()

	calls := 0

	got, err := retry.Do(context.Background(), io.Discard, 5, time.Nanosecond, func() (string, error) {
		calls++

		return "partial", retry.Permanent(errBoom)
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want errBoom unwrapped", err)
	}

	if got != "" {
		t.Fatalf("got = %q, want zero on permanent failure", got)
	}

	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (no retry on Permanent)", calls)
	}
}

func TestPermanent_NilStaysNil(t *testing.T) {
	t.Parallel()

	if retry.Permanent(nil) != nil {
		t.Fatal("Permanent(nil) should be nil so callers can wrap unconditionally")
	}
}

func TestDo_OnRetrySuppressesDefaultProgressLine(t *testing.T) {
	t.Parallel()

	var out strings.Builder

	hookCalls := 0

	_, _ = retry.Do(context.Background(), &out, 2, time.Nanosecond, func() (struct{}, error) {
		return struct{}{}, errBoom
	}, retry.OnRetry(func(_, _ int, _ time.Duration, _ error) {
		hookCalls++
	}))

	if hookCalls != 1 {
		t.Fatalf("hook calls = %d, want 1", hookCalls)
	}

	if out.String() != "" {
		t.Fatalf("default progress line should be suppressed when OnRetry is set, got %q", out.String())
	}
}

func TestAttemptsAndDelay_ClampNonPositiveToFallback(t *testing.T) {
	t.Parallel()

	if got := retry.Attempts(0, 3); got != 3 {
		t.Fatalf("Attempts(0,3) = %d", got)
	}

	if got := retry.Attempts(5, 3); got != 5 {
		t.Fatalf("Attempts(5,3) = %d", got)
	}

	if got := retry.Delay(0, 10*time.Second); got != 10*time.Second {
		t.Fatalf("Delay(0,10s) = %s", got)
	}

	if got := retry.Delay(2*time.Second, 10*time.Second); got != 2*time.Second {
		t.Fatalf("Delay(2s,10s) = %s", got)
	}
}

// TestDo_RefusesANonPositiveAttemptCount covers the case where the loop never
// runs.
//
// Do used to return the zero value and a nil error for attempts <= 0, which is
// byte-for-byte what success looks like: the operation never ran and no caller
// could tell. Callers are meant to clamp with Attempts(value, fallback) and
// most do, but Do is exported and takes a plain int — runMiseWithRetry, the raw
// container scan and the base-image evidence check each pass a caller-supplied
// count through unclamped. Zero at any of them would report a tool that
// installed, a scan that ran, or evidence that verified, none of which happened.
func TestDo_RefusesANonPositiveAttemptCount(t *testing.T) {
	t.Parallel()

	for _, attempts := range []int{0, -1, -100} {
		t.Run(strconv.Itoa(attempts), func(t *testing.T) {
			t.Parallel()

			called := 0

			got, err := retry.Do(t.Context(), nil, attempts, time.Millisecond,
				func() (string, error) {
					called++

					return "ran", nil
				})
			if err == nil {
				t.Fatalf("Do(attempts=%d) returned %q and no error; a skipped operation reads as success", attempts, got)
			}

			if !errors.Is(err, errs.ErrUsage) {
				t.Errorf("err = %v, want ErrUsage: the attempt count is the caller's mistake", err)
			}

			if called != 0 {
				t.Errorf("the operation ran %d times despite a refused attempt count", called)
			}

			if got != "" {
				t.Errorf("Do returned %q alongside the error", got)
			}
		})
	}
}

// TestAttempts_ClampsNegativeValues covers the clamp's other side. The existing
// test uses 0 and 5, so a guard written as `value != 0` passes both and hands a
// negative straight through to Do.
func TestAttempts_ClampsNegativeValues(t *testing.T) {
	t.Parallel()

	for _, value := range []int{-1, -7} {
		if got := retry.Attempts(value, 3); got != 3 {
			t.Errorf("Attempts(%d, 3) = %d, want the fallback 3", value, got)
		}
	}

	for _, value := range []time.Duration{-time.Second, -time.Nanosecond} {
		if got := retry.Delay(value, 10*time.Second); got != 10*time.Second {
			t.Errorf("Delay(%s, 10s) = %s, want the fallback", value, got)
		}
	}
}

// TestDo_HookSeesEachFailedAttemptWithItsOwnError asserts the whole hook call,
// not only the wait. Each attempt fails with a distinct error, so a hook handed
// a stale error, the wrong attempt number or the wrong total shows up as a
// mismatch -- and the hook must not fire for the attempt that succeeded.
func TestDo_HookSeesEachFailedAttemptWithItsOwnError(t *testing.T) {
	t.Parallel()

	failures := []error{errors.New("first"), errors.New("second")} //nolint:err113 // distinct fixture causes.

	type hookCall struct {
		attempt, total int
		wait           time.Duration
		err            error
	}

	var (
		calls []hookCall
		runs  int
	)

	got, err := retry.Do(context.Background(), io.Discard, 4, time.Nanosecond, func() (string, error) {
		runs++
		if runs <= len(failures) {
			return "", failures[runs-1]
		}

		return "done", nil
	}, retry.OnRetry(func(attempt, total int, wait time.Duration, err error) {
		calls = append(calls, hookCall{attempt, total, wait, err})
	}))
	if err != nil || got != "done" {
		t.Fatalf("Do = (%q, %v), want done", got, err)
	}

	want := []hookCall{
		{attempt: 1, total: 4, wait: time.Nanosecond, err: failures[0]},
		{attempt: 2, total: 4, wait: 2 * time.Nanosecond, err: failures[1]},
	}
	if !slices.Equal(calls, want) {
		t.Errorf("hook calls = %+v\nwant         %+v", calls, want)
	}
}

// TestDo_HookNeverFiresOnATerminalOutcome covers the three returns that end the
// loop: the last attempt failing, a Permanent error, and a first-try success. A
// hook firing on any of them announces a retry that does not happen.
func TestDo_HookNeverFiresOnATerminalOutcome(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		attempts  int
		fn        func() (struct{}, error)
		wantHooks int
	}{
		"exhaustion": {attempts: 3, fn: func() (struct{}, error) { return struct{}{}, errBoom }, wantHooks: 2},
		"permanent":  {attempts: 3, fn: func() (struct{}, error) { return struct{}{}, retry.Permanent(errBoom) }, wantHooks: 0},
		"success":    {attempts: 3, fn: func() (struct{}, error) { return struct{}{}, nil }, wantHooks: 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			hooks := 0

			_, _ = retry.Do(context.Background(), io.Discard, tc.attempts, time.Nanosecond, tc.fn,
				retry.OnRetry(func(int, int, time.Duration, error) { hooks++ }))

			if hooks != tc.wantHooks {
				t.Errorf("hook fired %d time(s), want %d", hooks, tc.wantHooks)
			}
		})
	}
}

// TestDo_WrappedPermanentStillStopsAndKeepsItsCause pins the nested case. A
// caller commonly adds context around the marker -- fmt.Errorf("push: %w",
// retry.Permanent(err)) -- and that must still stop the loop. What comes back
// is the marked cause itself, so errors.Is holds for it; the surrounding
// message is not carried, which is what "returns err unwrapped" means.
func TestDo_WrappedPermanentStillStopsAndKeepsItsCause(t *testing.T) {
	t.Parallel()

	runs := 0

	_, err := retry.Do(context.Background(), io.Discard, 5, time.Nanosecond, func() (int, error) {
		runs++

		return 0, fmt.Errorf("push: %w", retry.Permanent(errBoom))
	})

	if runs != 1 {
		t.Errorf("a wrapped Permanent was retried: %d run(s)", runs)
	}

	if !errors.Is(err, errBoom) || err.Error() != errBoom.Error() {
		t.Errorf("err = %v, want the marked cause %v itself", err, errBoom)
	}
}

// TestDo_OptionsApplyLeftToRight and TestRun_ForwardsTheOutcome close the two
// remaining documented contracts: a later option overrides an earlier one, and
// Run is Do with its value discarded, not a separate loop.
func TestDo_OptionsApplyLeftToRight(t *testing.T) {
	t.Parallel()

	var waits []time.Duration

	_ = retry.Run(context.Background(), io.Discard, 3, time.Nanosecond, func() error { return errBoom },
		retry.WithBackoff(retry.Constant), retry.WithBackoff(retry.Linear),
		retry.OnRetry(func(int, int, time.Duration, error) { t.Error("the first hook should have been replaced") }),
		retry.OnRetry(func(_, _ int, wait time.Duration, _ error) { waits = append(waits, wait) }))

	if want := []time.Duration{time.Nanosecond, 2 * time.Nanosecond}; !slices.Equal(waits, want) {
		t.Errorf("waits = %v, want the later Linear option's %v", waits, want)
	}
}

func TestRun_ForwardsTheOutcome(t *testing.T) {
	t.Parallel()

	runs := 0
	if err := retry.Run(context.Background(), io.Discard, 3, time.Nanosecond, func() error {
		runs++
		if runs < 2 {
			return errBoom
		}

		return nil
	}); err != nil || runs != 2 {
		t.Errorf("Run = %v after %d run(s), want nil after 2", err, runs)
	}

	if err := retry.Run(context.Background(), io.Discard, 2, time.Nanosecond, func() error { return errBoom }); !errors.Is(err, errBoom) {
		t.Errorf("Run err = %v, want errBoom", err)
	}
}
