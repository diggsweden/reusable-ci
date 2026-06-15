// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package httpretry

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

// DefaultClientTimeout is the per-request HTTP timeout API clients apply
// when REUSABLE_CI_HTTP_TIMEOUT is unset. It caps total wall time for one
// request including retries; the transport's MaxCumulativeDelay caps
// in-process backoff sleep separately.
const DefaultClientTimeout = 30 * time.Second

// timeoutEnv overrides DefaultClientTimeout with a Go duration string.
const timeoutEnv = "REUSABLE_CI_HTTP_TIMEOUT"

// ClientTimeout resolves the per-request HTTP timeout for API clients:
// REUSABLE_CI_HTTP_TIMEOUT parsed as a Go duration ("45s", "2m") when set
// and positive, otherwise DefaultClientTimeout. A malformed or
// non-positive override is ignored with a warning rather than failing the
// run — the default is always safe, and a slow link should not be fatal.
// clig.dev §Robustness asks to allow network timeouts to be configured
// with a reasonable default so it does not hang forever.
func ClientTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv(timeoutEnv))
	if raw == "" {
		return DefaultClientTimeout
	}

	d, err := time.ParseDuration(raw) //nolint:varnamelen // idiomatic short name for a duration.
	if err != nil || d <= 0 {
		slog.Warn("ignoring invalid "+timeoutEnv+"; using default",
			"value", raw, "default", DefaultClientTimeout.String())

		return DefaultClientTimeout
	}

	return d
}
