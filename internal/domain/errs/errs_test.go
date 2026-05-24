// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package errs_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
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
		{name: "unsupported", err: errs.ErrUnsupported, want: errs.ExitCodeUnavailable, wraps: true},
		{name: "usage", err: errs.ErrUsage, want: errs.ExitCodeUsage, wraps: true},
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

func TestFromHTTPStatus_MapsKnownClasses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		want   error // nil = unclassified
	}{
		{"200_ok_no_sentinel", 200, nil},
		{"301_redirect_no_sentinel", 301, nil},
		{"400_bad_request_no_sentinel", 400, nil},
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
