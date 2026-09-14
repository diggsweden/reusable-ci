// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package errs_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestExitCodeFromError_MapsSentinelsToExitCodes(t *testing.T) {
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
		{name: "rate_limited", err: errs.ErrRateLimited, want: errs.ExitCodeUnavailable, wraps: true},
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

// TestExitCodeFromError_AMalformedURLIsNotAnOutage covers the other thing a
// *url.Error can be: url.Parse's own failure, produced before any request
// exists. It implements net.Error like a dial failure does, so it exited 69
// and told CI to retry a URL that cannot parse. Only a url.Error whose cause is
// a network failure is one.
func TestExitCodeFromError_AMalformedURLIsNotAnOutage(t *testing.T) {
	t.Parallel()

	_, parseErr := url.Parse("http://[::1") //nolint:staticcheck // the parse failure is the fixture
	if parseErr == nil {
		t.Fatal("fixture URL parsed")
	}

	for name, err := range map[string]error{
		"bare parse error":    parseErr,
		"wrapped parse error": fmt.Errorf("build request: %w", parseErr),
	} {
		if got := errs.ExitCodeFromError(err); got == errs.ExitCodeUnavailable {
			t.Errorf("%s exits %d (unavailable), which asks CI to retry a URL that cannot parse", name, got)
		}
	}

	dial := &url.Error{Op: "Get", URL: "http://127.0.0.1:1/x", Err: &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}}
	if got := errs.ExitCodeFromError(dial); got != errs.ExitCodeUnavailable {
		t.Errorf("a url.Error wrapping a dial failure exits %d, want %d", got, errs.ExitCodeUnavailable)
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
		// Left unclassified, an adapter's fallback calls them
		// ErrDependencyUnavailable — exit 69, "retry me" — for what is
		// really "you asked for something invalid".
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

// TestExitCodeFromError_FilesystemErrorsAreNotNetworkErrors covers the
// operating-system errors no caller classified. syscall.Errno satisfies
// net.Error, so a missing file used to exit 69, "dependency unavailable",
// which CI reads as worth retrying. A missing path is EX_NOINPUT and a refused
// one EX_NOPERM, whether bare or wrapped; a socket failure carrying the same
// kind of errno inside *net.OpError is still a network error.
func TestExitCodeFromError_FilesystemErrorsAreNotNetworkErrors(t *testing.T) {
	t.Parallel()

	_, missing := os.Stat(filepath.Join(t.TempDir(), "absent"))

	cases := map[string]struct {
		err  error
		want errs.ExitCodeType
	}{
		"missing path":            {err: missing, want: errs.ExitCodeNoInput},
		"wrapped missing path":    {err: fmt.Errorf("read digest dir: %w", missing), want: errs.ExitCodeNoInput},
		"permission refused":      {err: &fs.PathError{Op: "open", Path: "/x", Err: syscall.EACCES}, want: errs.ExitCodeNoPerm},
		"unclassified errno":      {err: &fs.PathError{Op: "write", Path: "/x", Err: syscall.EIO}, want: errs.ExitCodeSoftware},
		"socket errno in OpError": {err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, want: errs.ExitCodeUnavailable},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, errs.ExitCodeFromError(testCase.err))
		})
	}
}

// TestCredentialRequired_NamesEveryWayToSupplyTheSecret covers the builder
// sixteen commands refuse through. The message has to say how to supply the
// credential, in both its forms, and the error has to be a usage error: a
// missing secret is the invocation's fault, exit 2, not a bug report.
func TestCredentialRequired_NamesEveryWayToSupplyTheSecret(t *testing.T) {
	t.Parallel()

	err := errs.CredentialRequired(
		errs.Credential{What: "release-bot token", Flag: "token-file", Env: "RELEASE_TOKEN"},
		errs.Credential{What: "signing key", Env: "GPG_PRIVATE_KEY"},
	)
	require.ErrorIs(t, err, errs.ErrUsage)
	require.Equal(t,
		`the release-bot token is required: pass --token-file <path> ("-" for stdin) or set $RELEASE_TOKEN; `+
			"the signing key is required: set $GPG_PRIVATE_KEY: usage error",
		err.Error())
}

// TestRuntimeRequired_NamesTheProviderOrNone: outside any CI job there is no
// detected provider, and the message says so rather than printing an empty
// name the operator would read as a rendering bug.
func TestRuntimeRequired_NamesTheProviderOrNone(t *testing.T) {
	t.Parallel()

	err := errs.RuntimeRequired("transfer run artifacts", "", []errs.EnvVar{{Name: "ACTIONS_RUNTIME_TOKEN", What: "runner-minted token"}})
	require.ErrorIs(t, err, errs.ErrCIRuntimeRequired)
	require.Contains(t, err.Error(), "Detected provider: none.")
	require.Contains(t, err.Error(), "ACTIONS_RUNTIME_TOKEN")

	require.Contains(t, errs.RuntimeRequired("x", "gitlab", nil).Error(), "Detected provider: gitlab.")
}
