// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package httpretry_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/httpretry"
	"github.com/stretchr/testify/require"
)

// Rewinding a request body is the step that makes a retry possible at all: the
// first send consumes the body, so every later attempt needs GetBody to hand
// back a fresh reader. When that fails there is nothing to retry WITH, and the
// transport has to stop rather than replay an empty or half-read body — which
// the server would accept as a different request.
//
// The existing test asserts the error is errs.ErrDependencyUnavailable, and
// that is the shared sentinel every transport failure carries: safeTransportError
// wraps whatever it is given. So it cannot tell a rewind failure from the inner
// transport failing on attempt two, which is a different bug with a different
// fix. A unique error identity is what separates them.

var (
	errRewindRefused   = errors.New("this body cannot be rewound") //nolint:err113 // the identity is the contract.
	errTransportBroken = errors.New("the inner transport gave up") //nolint:err113 // the identity is the contract.
)

func TestRoundTrip_ARewindFailureIsDistinguishableFromATransportFailure(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		rewindErr    error
		transportErr error
		want         error
		wantAttempts int
		wantRewinds  int
		why          string
	}{
		{
			name: "the body cannot be rewound", rewindErr: errRewindRefused, want: errRewindRefused,
			wantAttempts: 1, wantRewinds: 1,
			why: "one send, one failed rewind, then stop: replaying a body that could not be recreated would " +
				"send a different request than the caller wrote",
		},
		{
			name: "the inner transport fails on every attempt", transportErr: errTransportBroken, want: errTransportBroken,
			wantAttempts: 3, wantRewinds: 2,
			why: "the rewind worked; this is the other branch, and it must not be reported as a rewind failure",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			attempts, rewinds := 0, 0

			transport := httpretry.NewTransport(httpretry.Config{
				MaxAttempts: 3,
				BaseDelay:   time.Nanosecond,
				MaxDelay:    time.Nanosecond,
				Inner: roundTripFunc(func(*http.Request) (*http.Response, error) {
					attempts++

					if tc.transportErr != nil {
						return nil, tc.transportErr
					}

					return &http.Response{
						StatusCode: http.StatusServiceUnavailable,
						Status:     "503 Service Unavailable",
						Body:       io.NopCloser(strings.NewReader("try again")),
					}, nil
				}),
			})

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
				"https://example.invalid/resource", bytes.NewReader([]byte("body")))
			require.NoError(t, err)

			req.GetBody = func() (io.ReadCloser, error) {
				rewinds++

				if tc.rewindErr != nil {
					return nil, tc.rewindErr
				}

				return io.NopCloser(bytes.NewReader([]byte("body"))), nil
			}

			resp, err := transport.RoundTrip(req)
			if resp != nil {
				_ = resp.Body.Close()
			}

			require.ErrorIsf(t, err, tc.want,
				"the cause reaching the caller does not identify what actually failed: %s", tc.why)
			require.Equalf(t, tc.wantAttempts, attempts, "transport attempts: %s", tc.why)
			require.Equalf(t, tc.wantRewinds, rewinds, "body replays: %s", tc.why)
		})
	}
}

// A successful retry must replay the body, and the replayed body must be the
// same bytes. Without this the counts above are satisfied by a transport that
// calls GetBody and throws the result away.
func TestRoundTrip_AReplayedRequestCarriesTheSameBody(t *testing.T) {
	t.Parallel()

	var seen []string

	transport := httpretry.NewTransport(httpretry.Config{
		MaxAttempts: 2,
		BaseDelay:   time.Nanosecond,
		MaxDelay:    time.Nanosecond,
		Inner: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			seen = append(seen, string(body))

			if len(seen) == 1 {
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Status:     "503 Service Unavailable",
					Body:       io.NopCloser(strings.NewReader("try again")),
				}, nil
			}

			return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader("ok"))}, nil
		}),
	})

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		"https://example.invalid/resource", bytes.NewReader([]byte("the original body")))
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)

	_ = resp.Body.Close()

	require.Equal(t, []string{"the original body", "the original body"}, seen,
		"the retry sent a different body than the first attempt; the server would see two different requests")
}
