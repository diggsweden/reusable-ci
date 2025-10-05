// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/app/toolchain"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
)

// InstallMise fetches an archive over the network and writes an executable onto
// the runner. Everything between those two points is a refusal boundary, and
// the ones that matter are the ones that fail QUIETLY if they stop working: an
// oversized body read into memory, a corrupt archive that yields a truncated
// binary, a retry loop that turns a permanent 404 into four of them.
//
// These use an in-memory transport rather than a loopback server, which is what
// makes the transport-error and oversized-ContentLength cases reachable at all:
// a real server cannot easily lie about Content-Length or fail mid-dial on
// demand. It also keeps the test from opening a listener.

// roundTripFunc lets a test answer each request however it likes, including
// with a transport error.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func respondWith(status int, body []byte, contentLength int64) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: contentLength,
		Header:        make(http.Header),
	}
}

func installWith(t *testing.T, transport http.RoundTripper, archive []byte) (string, error) {
	t.Helper()

	return toolchain.InstallMise(context.Background(), &http.Client{Transport: transport}, &bytes.Buffer{},
		toolchain.InstallMiseInput{
			Version:          "2026.6.11",
			LinuxX64SHA256:   shaHexForArch(archive, "amd64"),
			LinuxARM64SHA256: shaHexForArch(archive, "arm64"),
			DestDir:          t.TempDir(),
			BaseURL:          "https://mise.invalid",
		})
}

func TestInstallMise_RefusesAndClassifiesEveryDownloadFailure(t *testing.T) {
	t.Parallel()

	archive := miseArchive(t, "#!/bin/sh\necho mise\n")

	for _, tc := range []struct {
		name         string
		transport    http.RoundTripper
		wantErr      error
		wantAttempts int
		why          string
	}{
		{
			name:         "a permanent 404 is not retried",
			wantAttempts: 1,
			transport:    fixedTransport(http.StatusNotFound, nil, 0, nil),
			wantErr:      errs.ErrDependencyUnavailable,
			why:          "a missing version does not become available by asking again; retrying it wastes the run's time",
		},
		{
			name:         "a 500 is retried to the attempt limit",
			wantAttempts: 4,
			transport:    fixedTransport(http.StatusInternalServerError, nil, 0, nil),
			wantErr:      errs.ErrDependencyUnavailable,
			why:          "a server-side failure is exactly what retrying is for",
		},
		{
			name:         "a 429 is retried",
			wantAttempts: 4,
			transport:    fixedTransport(http.StatusTooManyRequests, nil, 0, nil),
			wantErr:      errs.ErrDependencyUnavailable,
			why:          "rate limiting is transient by definition",
		},
		{
			name:         "a transport failure is retried",
			wantAttempts: 4,
			transport:    fixedTransport(0, nil, 0, errors.New("dial failed")), //nolint:err113 // injected transport failure.
			wantErr:      errs.ErrDependencyUnavailable,
			why:          "a dropped connection is the case retries exist for",
		},
		{
			name:         "an oversized Content-Length is refused before the body is read",
			wantAttempts: 1,
			transport:    fixedTransport(http.StatusOK, archive, (64<<20)+1, nil),
			wantErr:      errs.ErrMalformedInput,
			why:          "reading it to find out how big it is defeats the bound",
		},
		{
			name:         "a body larger than the bound is refused",
			wantAttempts: 1,
			transport:    fixedTransport(http.StatusOK, oversizedBody(t), -1, nil),
			wantErr:      errs.ErrMalformedInput,
			why:          "an unknown Content-Length must not become an unbounded read",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			counted := &countingTransport{inner: tc.transport}

			_, err := installWith(t, counted, archive)

			require.Errorf(t, err, "the download was accepted: %s", tc.why)
			require.ErrorIsf(t, err, tc.wantErr, "err = %v, want %v: %s", err, tc.wantErr, tc.why)
			require.Equalf(t, tc.wantAttempts, counted.calls,
				"%d attempts, want %d: %s", counted.calls, tc.wantAttempts, tc.why)
		})
	}
}

// A cancelled context must stop the run rather than spend the retry budget on
// a caller that has already given up — four attempts two seconds apart is six
// seconds a cancelled release run should not wait.
//
// The transport honours the context itself, as a real one does: net/http hands
// the request to the transport and relies on it to observe cancellation, so a
// fake that ignored ctx would be testing the fake.
func TestInstallMise_CancellationStopsTheRetryLoop(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	attempts := 0

	_, err := toolchain.InstallMise(ctx, &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts++

		if ctxErr := r.Context().Err(); ctxErr != nil {
			return nil, ctxErr
		}

		return respondWith(http.StatusOK, nil, 0), nil
	})}, &bytes.Buffer{}, toolchain.InstallMiseInput{
		Version:          "2026.6.11",
		LinuxX64SHA256:   strings.Repeat("a", 64),
		LinuxARM64SHA256: strings.Repeat("a", 64),
		DestDir:          t.TempDir(),
		BaseURL:          "https://mise.invalid",
	})

	require.Error(t, err, "a cancelled run reported success")
	require.LessOrEqual(t, attempts, 1,
		"a cancelled run made %d attempts; the retry loop kept going after the caller gave up", attempts)
}

// A corrupt or hostile archive must not leave a partial executable behind. The
// destination is checked after each refusal because a half-written binary at
// the install path is worse than no binary: the next step runs it.
func TestInstallMise_ArchiveRefusalsLeaveNoExecutable(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		archive []byte
		why     string
	}{
		{
			name: "not gzip at all", archive: []byte("this is not a gzip stream"),
			why: "a truncated or replaced download",
		},
		{
			name: "gzip carrying no tar", archive: gzipBytes(t, []byte("not a tar archive")),
			why: "valid compression around invalid contents",
		},
		{
			name: "a tar with no mise binary", archive: tarGzip(t, map[string]string{"mise/README": "nothing here"}),
			why: "an archive whose layout changed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dest := t.TempDir()

			_, err := toolchain.InstallMise(context.Background(),
				&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return respondWith(http.StatusOK, tc.archive, int64(len(tc.archive))), nil
				})}, &bytes.Buffer{}, toolchain.InstallMiseInput{
					Version:          "2026.6.11",
					LinuxX64SHA256:   shaHexForArch(tc.archive, "amd64"),
					LinuxARM64SHA256: shaHexForArch(tc.archive, "arm64"),
					DestDir:          dest,
					BaseURL:          "https://mise.invalid",
				})
			require.Errorf(t, err, "a bad archive installed something: %s", tc.why)

			entries, readErr := os.ReadDir(dest)
			require.NoError(t, readErr)

			for _, entry := range entries {
				t.Errorf("refusal left %q in the destination; the next step would run it", entry.Name())
			}

			_, statErr := os.Lstat(filepath.Join(dest, "mise"))
			require.ErrorIs(t, statErr, os.ErrNotExist)
		})
	}
}

// fixedTransport answers every request the same way. It takes the response
// PARTS rather than a *http.Response so the table above never constructs one:
// a response built and handed over is a body nobody closes, and the body is
// rebuilt per call anyway because a retry reads it again.
func fixedTransport(status int, body []byte, contentLength int64, err error) http.RoundTripper {
	return roundTripFunc(func(*http.Request) (*http.Response, error) {
		if err != nil {
			return nil, err
		}

		return respondWith(status, body, contentLength), nil
	})
}

// oversizedBody is one byte past the archive bound, served without a
// Content-Length so only the read limit can catch it.
func oversizedBody(t *testing.T) []byte {
	t.Helper()

	return bytes.Repeat([]byte("x"), (64<<20)+1)
}

// gzipBytes compresses arbitrary bytes, so a test can offer valid compression
// wrapped around something that is not a tar archive.
func gzipBytes(t *testing.T, body []byte) []byte {
	t.Helper()

	var out bytes.Buffer

	writer := gzip.NewWriter(&out)
	_, err := writer.Write(body)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	return out.Bytes()
}

// countingTransport records how many times a request was actually issued, which
// is the difference between "this failure is permanent" and "this failure is
// permanent and we asked four times anyway". A run that retries a 404 spends
// its retry budget, and the delay, on an answer that cannot change.
type countingTransport struct {
	inner http.RoundTripper
	calls int
}

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.calls++

	return c.inner.RoundTrip(r)
}
