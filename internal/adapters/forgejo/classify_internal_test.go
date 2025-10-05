// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo

import (
	"errors"
	"net/http"
	"testing"

	"code.gitea.io/sdk/gitea"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

var errForgejoTransport = errors.New("forgejo test: transport failure") //nolint:err113 // test fixture sentinel.

// classifyErr turns a Forgejo API failure into one of the project's sentinels,
// and every verb in this package routes its errors through it — so the sentinel
// it picks is the exit code a workflow sees, and what a retry loop decides to
// do. Nothing tested it directly: the coverage it had came incidentally from
// verb tests that each exercise one status.
//
// The distinction that matters most is between "the forge refused this request"
// and "the forge is unavailable". Getting it wrong in the unavailable direction
// makes a permanent refusal — a release that already exists, a token without
// the scope — look retryable, so a job retries until it times out instead of
// reporting what the server actually said.
func TestClassifyErr_MapsEveryStatusClass(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		status int
		want   error
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, want: errs.ErrPermissionDenied},
		{name: "forbidden", status: http.StatusForbidden, want: errs.ErrPermissionDenied},
		{name: "not found", status: http.StatusNotFound, want: errs.ErrMissingInput},
		{name: "rate limited", status: http.StatusTooManyRequests, want: errs.ErrRateLimited},
		{name: "conflict is a refusal, not an outage", status: http.StatusConflict, want: errs.ErrValidation},
		{name: "bad request", status: http.StatusBadRequest, want: errs.ErrValidation},
		{name: "unprocessable entity", status: http.StatusUnprocessableEntity, want: errs.ErrValidation},
		{name: "method not allowed", status: http.StatusMethodNotAllowed, want: errs.ErrValidation},
		{name: "internal server error", status: http.StatusInternalServerError, want: errs.ErrDependencyUnavailable},
		{name: "bad gateway", status: http.StatusBadGateway, want: errs.ErrDependencyUnavailable},
		{name: "service unavailable", status: http.StatusServiceUnavailable, want: errs.ErrDependencyUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resp := &gitea.Response{Response: &http.Response{StatusCode: tc.status}}

			got := classifyErr(resp, errForgejoTransport)
			if !errors.Is(got, tc.want) {
				t.Errorf("status %d classified as %v, want %v", tc.status, got, tc.want)
			}

			// The server's own error survives alongside the sentinel: the
			// classification is for the exit code, not a replacement for what
			// the forge said.
			if !errors.Is(got, errForgejoTransport) {
				t.Errorf("status %d lost the underlying cause: %v", tc.status, got)
			}
		})
	}
}

// TestClassifyErr_WithoutAResponseIsUnavailable covers the transport-level
// case: no HTTP response at all means the request never reached a server, which
// is the one situation that genuinely is a dependency being unavailable.
func TestClassifyErr_WithoutAResponseIsUnavailable(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		resp *gitea.Response
	}{
		{name: "no response value"},
		{name: "a response wrapper with no HTTP response", resp: &gitea.Response{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := classifyErr(tc.resp, errForgejoTransport)
			if !errors.Is(got, errs.ErrDependencyUnavailable) {
				t.Errorf("classified as %v, want ErrDependencyUnavailable", got)
			}
		})
	}
}

// TestClassifyErr_NilErrorStaysNil keeps a successful call from acquiring a
// sentinel because the response happened to carry an error status.
func TestClassifyErr_NilErrorStaysNil(t *testing.T) {
	t.Parallel()

	resp := &gitea.Response{Response: &http.Response{StatusCode: http.StatusInternalServerError}}
	if got := classifyErr(resp, nil); got != nil {
		t.Errorf("classifyErr(_, nil) = %v, want nil", got)
	}
}
