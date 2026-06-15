// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package httpretry_test

import (
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/internal/adapters/httpretry"
)

func TestClientTimeout(t *testing.T) {
	// Not parallel: mutates a process env var via t.Setenv.
	for _, tc := range []struct {
		name string
		set  bool
		val  string
		want time.Duration
	}{
		{name: "unset uses default", set: false, want: httpretry.DefaultClientTimeout},
		{name: "valid override", set: true, val: "45s", want: 45 * time.Second},
		{name: "valid minutes", set: true, val: "2m", want: 2 * time.Minute},
		{name: "malformed falls back", set: true, val: "soon", want: httpretry.DefaultClientTimeout},
		{name: "zero falls back", set: true, val: "0s", want: httpretry.DefaultClientTimeout},
		{name: "negative falls back", set: true, val: "-5s", want: httpretry.DefaultClientTimeout},
		{name: "blank falls back", set: true, val: "   ", want: httpretry.DefaultClientTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv("REUSABLE_CI_HTTP_TIMEOUT", tc.val)
			} else {
				// Ensure a leaked value from the environment can't skew this case.
				t.Setenv("REUSABLE_CI_HTTP_TIMEOUT", "")
			}

			if got := httpretry.ClientTimeout(); got != tc.want {
				t.Errorf("ClientTimeout() = %v, want %v", got, tc.want)
			}
		})
	}
}
