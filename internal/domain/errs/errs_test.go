// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package errs_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestExitCodeFromError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		err   error
		want  errs.ExitCodeType
		wraps bool
	}{
		{name: "nil", want: errs.ExitCodeOK},
		{name: "context_canceled", err: context.Canceled, want: errs.ExitCodeUsage, wraps: true},
		{name: "context_deadline_exceeded", err: context.DeadlineExceeded, want: errs.ExitCodeUnavailable, wraps: true},
		{name: "unsupported", err: errs.ErrUnsupported, want: errs.ExitCodeConfiguration, wraps: true},
		{name: "usage", err: errs.ErrUsage, want: errs.ExitCodeUsage, wraps: true},
		{name: "ci_runtime_required", err: errs.ErrCIRuntimeRequired, want: errs.ExitCodeUsage, wraps: true},
		{name: "ci_runtime_built", err: errs.RuntimeRequired("transfer run artifacts", "github", []errs.EnvVar{{Name: "ACTIONS_RUNTIME_TOKEN", What: "x"}}), want: errs.ExitCodeUsage},
		{name: "invalid_config", err: errs.ErrInvalidConfig, want: errs.ExitCodeConfiguration, wraps: true},
		{name: "missing_input", err: errs.ErrMissingInput, want: errs.ExitCodeNoInput, wraps: true},
		{name: "release_not_found", err: errs.ErrReleaseNotFound, want: errs.ExitCodeNoInput},
		{name: "validation", err: errs.ErrValidation, want: errs.ExitCodeValidation, wraps: true},
		{name: "malformed_input", err: errs.ErrMalformedInput, want: errs.ExitCodeDataErr, wraps: true},
		{name: "permission_denied", err: errs.ErrPermissionDenied, want: errs.ExitCodeNoPerm, wraps: true},
		{name: "dependency_unavailable", err: errs.ErrDependencyUnavailable, want: errs.ExitCodeUnavailable, wraps: true},
		{name: "plain_error", err: errors.New("boom"), want: errs.ExitCodeSoftware}, //nolint:err113 // test mock error
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, errs.ExitCodeFromError(testCase.err))

			if testCase.wraps {
				require.Equal(t, testCase.want, errs.ExitCodeFromError(fmt.Errorf("wrapped: %w", testCase.err)))
			}
		})
	}
}

// TestExitCodeFromError_NetworkErrorsAreUnavailable proves a transport-level
// failure (connection refused, DNS failure, a url.Error wrapping either) maps
// to ExitCodeUnavailable (69) — "the forge/registry was unreachable" — and not
// ExitCodeSoftware (70), which would tell CI to file a bug. Covers the raw
// errors and the adapter's `fmt.Errorf("...: %w", err)` wrap shape.
func TestExitCodeFromError_NetworkErrorsAreUnavailable(t *testing.T) {
	t.Parallel()

	refused := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")} //nolint:err113 // test mock error
	dnsFail := &net.DNSError{Err: "no such host", Name: "nonexistent.invalid"}
	urlErr := &url.Error{Op: "Get", URL: "http://127.0.0.1:1/x", Err: refused}

	cases := []struct {
		name string
		err  error
	}{
		{name: "dial_refused", err: refused},
		{name: "dns_failure", err: dnsFail},
		{name: "url_error_wrapping_dial", err: urlErr},
		{name: "adapter_wrapped", err: fmt.Errorf("list run 1 artifacts: %w", urlErr)},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, errs.ExitCodeUnavailable, errs.ExitCodeFromError(testCase.err))
		})
	}
}

// TestExitCodeFromError_SentinelWinsOverNetwork guards the case ordering: an
// error that is both wrapped with a domain sentinel and a network error must
// keep its sentinel classification (the net.Error check runs last).
func TestExitCodeFromError_SentinelWinsOverNetwork(t *testing.T) {
	t.Parallel()

	netErr := &net.OpError{Op: "dial", Err: errors.New("refused")} //nolint:err113 // test mock error
	// 401 → ErrPermissionDenied wrapped around a transport error.
	wrapped := fmt.Errorf("auth failed (%w): %w", errs.ErrPermissionDenied, netErr)

	require.Equal(t, errs.ExitCodeNoPerm, errs.ExitCodeFromError(wrapped))
}

func TestFromHTTPStatus_MapsKnownClasses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		want   error // nil = unclassified
	}{
		{"200_ok_no_sentinel", 200, nil},
		{"301_redirect_no_sentinel", 301, nil},
		// A 4xx is the request being refused by a server that answered.
		// These previously returned nil, and every adapter's fallback then
		// called them ErrDependencyUnavailable — exit 69, "retry me" — for
		// what is really "you asked for something invalid".
		{"400_bad_request_is_validation", 400, errs.ErrValidation},
		{"405_method_not_allowed_is_validation", 405, errs.ErrValidation},
		{"409_conflict_is_validation", 409, errs.ErrValidation},
		{"422_unprocessable_is_validation", 422, errs.ErrValidation},
		{"401_unauthorized_is_perm", 401, errs.ErrPermissionDenied},
		{"403_forbidden_is_perm", 403, errs.ErrPermissionDenied},
		{"404_not_found_is_missing", 404, errs.ErrMissingInput},
		{"429_rate_limit_is_rate_limited", 429, errs.ErrRateLimited},
		{"500_server_err_is_dep_unavail", 500, errs.ErrDependencyUnavailable},
		{"503_service_unavail_is_dep_unavail", 503, errs.ErrDependencyUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := errs.FromHTTPStatus(tc.status)
			if tc.want == nil {
				require.NoError(t, got)
			} else {
				require.ErrorIs(t, got, tc.want)
			}
		})
	}
}

func TestErrReleaseNotFound_IsSentinel(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("check tag v1.0.0: %w", errs.ErrReleaseNotFound)
	require.ErrorIs(t, wrapped, errs.ErrReleaseNotFound)
}

func TestExitCodeConstants_AlignToBSDSysexits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  errs.ExitCodeType
		want int
	}{
		{"ok", errs.ExitCodeOK, 0},
		{"validation", errs.ExitCodeValidation, 1},
		{"usage", errs.ExitCodeUsage, 2},
		{"dataerr", errs.ExitCodeDataErr, 65},
		{"noinput", errs.ExitCodeNoInput, 66},
		{"unavailable", errs.ExitCodeUnavailable, 69},
		{"software", errs.ExitCodeSoftware, 70},
		{"noperm", errs.ExitCodeNoPerm, 77},
		{"configuration", errs.ExitCodeConfiguration, 78},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, int(tc.got))
		})
	}
}

// A permanent condition must not share an exit code with the transient ones.
//
// This is the property, stated separately from the table above because the
// table pins values while this pins the *reason* they differ: 69 tells a caller
// to try again, and a capability the platform does not implement never becomes
// available by trying again. The same confusion has now been found three times
// — a refused token classified as an outage (PAR-TOK-2), a missing manifest
// reported as one by the registry adapter, and this — so it is worth a test that
// fails on intent rather than on a number.
func TestExitCode_UnsupportedIsNotRetryable(t *testing.T) {
	t.Parallel()

	unsupported := errs.ExitCodeFromError(errs.ErrUnsupported)

	for _, transient := range []struct {
		name string
		err  error
	}{
		{"dependency unavailable", errs.ErrDependencyUnavailable},
		{"rate limited", errs.ErrRateLimited},
	} {
		if got := errs.ExitCodeFromError(transient.err); got == unsupported {
			t.Errorf("unsupported exits %d, the same as %s — a caller cannot tell a permanent gap from one worth retrying",
				unsupported, transient.name)
		}
	}

	if unsupported != errs.ExitCodeConfiguration {
		t.Errorf("unsupported exits %d, want %d (EX_CONFIG): asking a forge for a capability it lacks is a configuration mismatch",
			unsupported, errs.ExitCodeConfiguration)
	}
}
