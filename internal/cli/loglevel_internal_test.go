// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli

import (
	"testing"
)

// TestResolveLogLevel exercises the precedence matrix from the
// configureLogger doc: quiet > explicit flag/env > $DEBUG > default.
//
// Pure-function test — no global slog state, no env reads, no
// t.Parallel() races against other suites that mutate slog.Default.
func TestResolveLogLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		flagLevel   string
		flagSet     bool
		quiet       bool
		debugTruthy bool
		want        string
	}{
		{name: "default → info", flagLevel: "info", want: "info"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{name: "explicit flag wins", flagLevel: "warn", flagSet: true, want: "warn"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{name: "DEBUG bumps default info → debug", flagLevel: "info", debugTruthy: true, want: "debug"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{name: "DEBUG ignored when flag explicit", flagLevel: "error", flagSet: true, debugTruthy: true, want: "error"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{name: "DEBUG ignored when flag value non-info", flagLevel: "warn", flagSet: false, debugTruthy: true, want: "warn"},
		{name: "quiet wins over DEBUG", flagLevel: "info", quiet: true, debugTruthy: true, want: "error"},
		{name: "quiet wins over explicit flag", flagLevel: "debug", flagSet: true, quiet: true, want: "error"},
		{name: "DEBUG falsy leaves default", flagLevel: "info", debugTruthy: false, want: "info"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := resolveLogLevel(tc.flagLevel, tc.flagSet, tc.quiet, tc.debugTruthy)
			if got != tc.want {
				t.Errorf("resolveLogLevel(%q, flagSet=%v, quiet=%v, debug=%v) = %q, want %q",
					tc.flagLevel, tc.flagSet, tc.quiet, tc.debugTruthy, got, tc.want)
			}
		})
	}
}

func TestIsTruthyEnv(t *testing.T) {
	for _, tc := range []struct {
		val  string
		want bool
	}{
		{"1", true},
		{"true", true},
		{"yes", true},
		{"on", true},
		{"True", true},
		{"YES", true},
		{"On", true},
		{"0", false},
		{"false", false},
		{"no", false},
		{"off", false},
		{"", false},
		{"random", false},
	} {
		t.Run(tc.val, func(t *testing.T) {
			t.Setenv("REUSABLE_CI_TRUTHY_TEST", tc.val)

			got := isTruthyEnv("REUSABLE_CI_TRUTHY_TEST")
			if got != tc.want {
				t.Errorf("isTruthyEnv(%q) = %v, want %v", tc.val, got, tc.want)
			}
		})
	}
}
