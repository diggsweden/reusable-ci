// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package httpretry

import (
	"testing"
	"time"
)

// TestComputeDelay_SaturatesInsteadOfOverflowing covers the exponential
// backoff at the attempt counts where a shift stops behaving like a doubling.
//
// The delay is computed by shifting the base Duration left once per attempt,
// which is an int64 shift. With a one-second base the product passes MaxInt64
// at attempt 34 and comes back negative; the cap did not catch that (a negative
// value is not greater than MaxDelay), so rand.Int64N received a non-positive
// bound and PANICKED — inside a retry, which is the one place a caller has
// asked the code not to fail. Past attempt 62 the shift reaches zero instead,
// and a zero backoff is a hot loop against a server that is already refusing.
//
// Neither is reachable with the retry counts this package ships, and both are
// reachable from a Config an adopter supplies. The delay is random, so the
// assertion is the invariant rather than a value: always within [0, MaxDelay],
// never a panic.
func TestComputeDelay_SaturatesInsteadOfOverflowing(t *testing.T) {
	t.Parallel()

	for _, cfg := range []struct {
		name     string
		base     time.Duration
		maxDelay time.Duration
	}{
		{name: "one second base", base: time.Second, maxDelay: 30 * time.Second},
		{name: "millisecond base", base: time.Millisecond, maxDelay: time.Minute},
		{name: "base already above the cap", base: time.Hour, maxDelay: time.Second},
		{name: "zero base", base: 0, maxDelay: time.Second},
		{name: "zero cap", base: time.Second, maxDelay: 0},
	} {
		t.Run(cfg.name, func(t *testing.T) {
			t.Parallel()

			transport := &Transport{cfg: Config{BaseDelay: cfg.base, MaxDelay: cfg.maxDelay}}

			// Well past every shift boundary: 34 is where a one-second base
			// overflows, 63 and 64 are where the shift itself vanishes.
			for _, attempt := range []int{0, 1, 5, 30, 33, 34, 35, 40, 62, 63, 64, 100, 1000} {
				got := transport.computeDelay(attempt, nil)

				if got < 0 {
					t.Errorf("attempt %d: delay = %v, want a non-negative duration", attempt, got)
				}

				if got > cfg.maxDelay {
					t.Errorf("attempt %d: delay = %v, want at most the configured cap %v", attempt, got, cfg.maxDelay)
				}
			}
		})
	}
}
