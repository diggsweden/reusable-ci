// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package retry is the single source of truth for the attempt/backoff retry
// loop shared across the CLI's retriable operations — container build/push and
// signer-image registry pushes (the original callers), plus container scans,
// base-image evidence verification, and mise tool download/install. Each caller
// supplies its own attempt/delay defaults via Attempts/Delay and tunes the loop
// through Options (backoff shape, a per-attempt hook, non-retriable errors); the
// loop, its context-cancellation handling, and its backoff math live here once.
package retry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// Backoff selects how the wait between attempts grows.
type Backoff int

const (
	// Linear waits attempt×delay before retry N — the escalating backoff the
	// package was built for (registry pushes). It is the zero value, so a
	// caller that passes no Options keeps the historical behavior.
	Linear Backoff = iota
	// Constant waits a fixed delay between every attempt.
	Constant
)

// config is the resolved Option set for one Do call. Its zero value is the
// historical default: linear backoff, every error retried, and a generic
// progress line written to out before each wait.
type config struct {
	backoff Backoff
	onRetry func(attempt, attempts int, wait time.Duration, err error)
}

// Option tunes a retry loop. Options are applied left to right.
type Option func(*config)

// WithBackoff selects the backoff growth (default Linear).
func WithBackoff(b Backoff) Option {
	return func(c *config) { c.backoff = b }
}

// OnRetry replaces the default "Command failed (attempt n/m)…" progress line
// with a custom callback, invoked once before each wait with the failing
// attempt's error and the computed wait. Use it when a caller wants richer
// per-attempt diagnostics than the generic line. When set, out is not written
// to by the loop itself.
func OnRetry(fn func(attempt, attempts int, wait time.Duration, err error)) Option {
	return func(c *config) { c.onRetry = fn }
}

// permanent marks an error as non-retriable.
type permanent struct{ err error }

func (p permanent) Error() string { return p.err.Error() }
func (p permanent) Unwrap() error { return p.err }

// Permanent wraps err so Do stops immediately and returns err unwrapped,
// without consuming further attempts. Return it from fn when the failure is
// not worth retrying (e.g. an HTTP 4xx, a validation error). Permanent(nil)
// returns nil so callers can wrap unconditionally.
func Permanent(err error) error {
	if err == nil {
		return nil
	}

	return permanent{err: err}
}

// Do runs fn until it returns a nil error or attempts is exhausted, waiting
// between tries per the selected backoff (attempt×delay by default) and
// preventing later attempts if ctx is cancelled. The first attempt still runs
// with a pre-cancelled context. It returns fn's last error on
// exhaustion, the unwrapped error when fn returns a Permanent(err), or
// ctx.Err() on cancellation. When out is non-nil and no OnRetry hook is set, a
// generic progress line is written before each wait.
//
//nolint:cyclop // the single-source retry loop deliberately handles success, permanent errors, exhaustion, backoff, and cancellation in one place.
func Do[T any](ctx context.Context, out io.Writer, attempts int, delay time.Duration, fn func() (T, error), opts ...Option) (T, error) {
	cfg := config{backoff: Linear}
	for _, opt := range opts {
		opt(&cfg)
	}

	// A non-positive attempt count used to skip the loop entirely and return
	// the zero value with a nil error — the operation never ran and the caller
	// could not tell, because that is exactly what success looks like here.
	//
	// Callers are meant to clamp with Attempts(value, fallback), and most do,
	// but Do is exported and takes a plain int: runMiseWithRetry, the raw
	// container scan and the base-image evidence check all pass a caller-
	// supplied count straight through. Any of them reaching zero would report
	// a tool that installed, a scan that ran, or evidence that verified, none
	// of which happened. Refusing is the fail-closed reading, and it names the
	// caller's bug rather than absorbing it.
	if attempts < 1 {
		var zero T

		return zero, fmt.Errorf("retry: attempts must be at least 1, got %d: %w", attempts, errs.ErrUsage)
	}

	var err error

	for attempt := 1; attempt <= attempts; attempt++ {
		// Preserve the initial-attempt policy, but never start another attempt
		// after cancellation, even when the delay or retry hook is zero-time.
		if attempt > 1 && ctx.Err() != nil {
			var zero T

			return zero, ctx.Err()
		}

		result, runErr := fn()
		if runErr == nil {
			return result, nil
		}

		var perm permanent
		if errors.As(runErr, &perm) {
			var zero T

			return zero, perm.err
		}

		err = runErr

		if attempt == attempts {
			break
		}

		wait := backoffWait(cfg.backoff, attempt, delay)
		if cfg.onRetry != nil {
			cfg.onRetry(attempt, attempts, wait, runErr)
		} else if out != nil {
			_, _ = fmt.Fprintf(out, "Command failed (attempt %d/%d), retrying in %s...\n", attempt, attempts, wait)
		}

		if wait <= 0 {
			continue
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}

			var zero T

			return zero, ctx.Err()
		case <-timer.C:
		}
	}

	var zero T

	return zero, err
}

// backoffWait computes the wait before the retry that follows a failed attempt,
// saturating positive linear overflow at the largest representable duration.
func backoffWait(b Backoff, attempt int, delay time.Duration) time.Duration {
	if b == Constant {
		return delay
	}

	if delay > 0 && attempt > 0 && int64(attempt) > math.MaxInt64/int64(delay) {
		return time.Duration(math.MaxInt64)
	}

	return time.Duration(attempt) * delay
}

// Run is Do for operations that yield no value.
func Run(ctx context.Context, out io.Writer, attempts int, delay time.Duration, fn func() error, opts ...Option) error {
	_, err := Do(ctx, out, attempts, delay, func() (struct{}, error) {
		return struct{}{}, fn()
	}, opts...)

	return err
}

// Attempts clamps a configured attempt count to fallback when non-positive.
func Attempts(value, fallback int) int {
	if value > 0 {
		return value
	}

	return fallback
}

// Delay clamps a configured retry delay to fallback when non-positive.
func Delay(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}

	return fallback
}
