// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package retry is the single source of truth for the linear-backoff retry loop
// shared by the container build/push and signer-image verbs, whose registry
// pushes are the operations worth retrying. Each caller supplies its own
// attempt/delay defaults via Attempts/Delay; the loop itself lives here once.
package retry

import (
	"context"
	"fmt"
	"io"
	"time"
)

// Do runs fn until it returns a nil error or attempts is exhausted, waiting
// attempt×delay between tries (linear backoff) and aborting immediately if ctx
// is cancelled. It returns fn's last error on exhaustion, or ctx.Err() on
// cancellation. When out is non-nil a progress line is written before each wait.
func Do[T any](ctx context.Context, out io.Writer, attempts int, delay time.Duration, fn func() (T, error)) (T, error) {
	var err error

	for attempt := 1; attempt <= attempts; attempt++ {
		result, runErr := fn()
		if runErr == nil {
			return result, nil
		}

		err = runErr
		if attempt == attempts {
			break
		}

		wait := time.Duration(attempt) * delay
		if out != nil {
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

// Run is Do for operations that yield no value.
func Run(ctx context.Context, out io.Writer, attempts int, delay time.Duration, fn func() error) error {
	_, err := Do(ctx, out, attempts, delay, func() (struct{}, error) {
		return struct{}{}, fn()
	})

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
