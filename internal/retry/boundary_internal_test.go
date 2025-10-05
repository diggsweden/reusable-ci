// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package retry

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func TestRetryBoundary_CancellationIndependentOfDelay(t *testing.T) {
	t.Parallel()

	for _, delay := range []time.Duration{0, time.Hour} {
		for _, preCancelled := range []bool{false, true} {
			ctx, cancel := context.WithCancel(t.Context())
			if preCancelled {
				cancel()
			}

			calls := 0
			cause := errors.New("owned retry cause") //nolint:err113 // distinct callback failure, never a dependency call.
			_, err := Do(ctx, nil, 3, delay, func() (int, error) {
				calls++

				cancel()

				return 9, cause
			})

			cancel()

			if !errors.Is(err, context.Canceled) || calls != 1 {
				t.Fatalf("delay=%s precancel=%v calls=%d err=%v", delay, preCancelled, calls, err)
			}
		}
	}

	ctx, cancel := context.WithDeadline(t.Context(), time.Unix(0, 0))
	defer cancel()

	calls := 0

	_, err := Do(ctx, nil, 3, 0, func() (int, error) {
		calls++

		return 0, context.DeadlineExceeded
	})
	if !errors.Is(err, context.DeadlineExceeded) || calls != 1 {
		t.Fatalf("deadline calls=%d err=%v", calls, err)
	}
}

func TestRetryBoundary_SaturatesLinearOverflow(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		attempt     int
		delay, want time.Duration
	}{
		{1, time.Duration(math.MaxInt64), time.Duration(math.MaxInt64)},
		{2, time.Duration(1 << 62), time.Duration(math.MaxInt64)},
		{2, time.Duration(math.MaxInt64 / 2), time.Duration(math.MaxInt64 - 1)},
		{3, time.Second, 3 * time.Second},
	} {
		if got := backoffWait(Linear, tc.attempt, tc.delay); got != tc.want {
			t.Fatalf("attempt=%d delay=%d got=%d want=%d", tc.attempt, tc.delay, got, tc.want)
		}
	}

	if got := backoffWait(Constant, 2, time.Duration(1<<62)); got != time.Duration(1<<62) {
		t.Fatalf("constant delay changed: %d", got)
	}
}
