// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package retry_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/retry"
)

var errBoom = errors.New("boom")

func TestDo_ReturnsFirstSuccessWithoutRetrying(t *testing.T) {
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
	got, err := retry.Do(context.Background(), io.Discard, 2, time.Nanosecond, func() (string, error) {
		return "partial", errBoom
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want errBoom", err)
	}

	if got != "" {
		t.Fatalf("got = %q, want zero value on failure", got)
	}
}

func TestDo_AbortsOnContextCancellation(t *testing.T) {
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

func TestAttemptsAndDelayClampNonPositiveToFallback(t *testing.T) {
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
