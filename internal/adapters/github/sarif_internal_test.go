// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// sarifSent is one request as the transport saw it.
type sarifSent struct {
	target string
	auth   string
	body   string
}

// sarifProvider answers every request with respond and records what was sent.
func sarifProvider(t *testing.T, respond func(req *http.Request, n int) (*http.Response, error)) (*Provider, *[]sarifSent) {
	t.Helper()

	var sent []sarifSent

	return &Provider{
		APIBaseOverride: "https://api.github.invalid",
		HTTPClient: &http.Client{Transport: contractTransport(func(req *http.Request) (*http.Response, error) {
			var body []byte
			if req.Body != nil {
				body, _ = io.ReadAll(req.Body)
			}

			sent = append(sent, sarifSent{target: req.Method + " " + req.URL.String(), auth: req.Header.Get("Authorization"), body: string(body)})

			return respond(req, len(sent))
		})},
	}, &sent
}

func sarifUpload(repository string) provider.SARIFUpload {
	return provider.SARIFUpload{
		SARIF: []byte(`{"version":"2.1.0"}`), Token: "tok", Repository: repository,
		SHA: "0123456789abcdef0123456789abcdef01234567", Ref: "refs/heads/main",
	}
}

var errConnectionReset = errors.New("connection reset")

const sarifEndpoint = "POST https://api.github.invalid/repos/owner/repo/code-scanning/sarifs"

// TestUploadSARIF_ResponsesAndTransportFailuresAreClassified covers the
// statuses the server-rejection test does not, each after exactly one
// request, and a transport failure that keeps its cause.
func TestUploadSARIF_ResponsesAndTransportFailuresAreClassified(t *testing.T) {
	t.Parallel()

	for status, want := range map[int]error{
		http.StatusAccepted:            nil,
		http.StatusBadRequest:          errs.ErrValidation,
		http.StatusUnprocessableEntity: errs.ErrValidation,
		http.StatusNotFound:            errs.ErrMissingInput,
		http.StatusTooManyRequests:     errs.ErrRateLimited,
		http.StatusServiceUnavailable:  errs.ErrDependencyUnavailable,
	} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()

			p, sent := sarifProvider(t, func(req *http.Request, _ int) (*http.Response, error) {
				return contractResponse(req, status, `{"message":"answer"}`), nil
			})

			err := p.UploadSARIF(t.Context(), sarifUpload("owner/repo"))
			require.Len(t, *sent, 1)
			require.Equal(t, sarifEndpoint, (*sent)[0].target)

			if want == nil {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, want)
			require.ErrorContains(t, err, "HTTP "+strconv.Itoa(status)+`: {"message":"answer"}`)
		})
	}

	p, sent := sarifProvider(t, func(*http.Request, int) (*http.Response, error) { return nil, errConnectionReset })

	require.ErrorIs(t, p.UploadSARIF(t.Context(), sarifUpload("owner/repo")), errConnectionReset)
	require.Len(t, *sent, 1)
}

// TestUploadSARIF_RedirectsStayOnTheAPIAuthority: a redirect on the API host
// is followed with the token and the report replayed. One that changes host,
// scheme or adds userinfo is refused before the report is sent there; the
// standard library would otherwise have posted it without the token and read
// the other host's 2xx as an accepted upload.
func TestUploadSARIF_RedirectsStayOnTheAPIAuthority(t *testing.T) {
	t.Parallel()

	p, sent := sarifProvider(t, func(req *http.Request, n int) (*http.Response, error) {
		if n == 1 {
			resp := contractResponse(req, http.StatusTemporaryRedirect, "")
			resp.Header.Set("Location", "https://api.github.invalid/moved/sarifs")

			return resp, nil
		}

		return contractResponse(req, http.StatusAccepted, `{}`), nil
	})

	require.NoError(t, p.UploadSARIF(t.Context(), sarifUpload("owner/repo")))
	require.Len(t, *sent, 2)
	require.Equal(t, "POST https://api.github.invalid/moved/sarifs", (*sent)[1].target)
	require.Equal(t, "token tok", (*sent)[1].auth)
	require.Equal(t, (*sent)[0].body, (*sent)[1].body)

	for _, location := range []string{
		"https://elsewhere.invalid/repos/owner/repo/code-scanning/sarifs",
		"http://api.github.invalid/repos/owner/repo/code-scanning/sarifs",
		"https://user@api.github.invalid/repos/owner/repo/code-scanning/sarifs",
	} {
		t.Run(location, func(t *testing.T) {
			t.Parallel()

			p, sent := sarifProvider(t, func(req *http.Request, _ int) (*http.Response, error) {
				resp := contractResponse(req, http.StatusTemporaryRedirect, "")
				resp.Header.Set("Location", location)

				return resp, nil
			})

			err := p.UploadSARIF(t.Context(), sarifUpload("owner/repo"))
			require.ErrorIs(t, err, errs.ErrValidation)
			require.Len(t, *sent, 1, "the report followed the redirect")
		})
	}
}

// TestUploadSARIF_MalformedRepositoryIsRefusedBeforeAnyRequest: the
// repository is spliced into the request path, so anything other than two
// path-safe segments is a usage error with nothing sent.
func TestUploadSARIF_MalformedRepositoryIsRefusedBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	for _, repository := range []string{"owner", "owner/repo/extra", "../repo", "owner/..", "owner/re po", "owner/repo?x=1"} {
		t.Run(repository, func(t *testing.T) {
			t.Parallel()

			p, sent := sarifProvider(t, func(req *http.Request, _ int) (*http.Response, error) {
				return contractResponse(req, http.StatusAccepted, `{}`), nil
			})

			require.ErrorIs(t, p.UploadSARIF(t.Context(), sarifUpload(repository)), errs.ErrUsage)
			require.Empty(t, *sent)
		})
	}
}
