// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/app/toolchain"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func memoryResponse(req *http.Request, status int, header http.Header, body []byte) *http.Response {
	if header == nil {
		header = http.Header{}
	}

	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: req}
}

// TestInstallMise_DefaultSourceThroughMemoryTransport installs from the default
// source without a network: the only first request is an HTTPS GET for the
// pinned release asset on github.com, and a redirect to the asset host is
// followed because the pinned SHA-256 decides what is installed. The same
// redirect to bytes that do not match the pin installs nothing.
func TestInstallMise_DefaultSourceThroughMemoryTransport(t *testing.T) {
	t.Parallel()

	archive := miseArchive(t, "#!/bin/sh\necho mise\n")
	wantFirst := "https://github.com/jdx/mise/releases/download" + expectedArchivePath()

	for _, tc := range []struct {
		name    string
		served  []byte
		wantErr error
	}{
		{name: "pinned bytes", served: archive},
		{name: "other bytes", served: miseArchive(t, "#!/bin/sh\necho substituted\n"), wantErr: errs.ErrValidation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var requests []string

			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests = append(requests, req.Method+" "+req.URL.String())

				if req.URL.Host == "github.com" {
					return memoryResponse(req, http.StatusFound, http.Header{"Location": {"https://objects.example.invalid/asset?sig=x"}}, nil), nil
				}

				return memoryResponse(req, http.StatusOK, nil, tc.served), nil
			})}

			dest := t.TempDir()

			path, err := toolchain.InstallMise(context.Background(), client, &bytes.Buffer{}, toolchain.InstallMiseInput{
				Version: "2026.6.11", LinuxX64SHA256: shaHexForArch(archive, "amd64"), LinuxARM64SHA256: shaHexForArch(archive, "arm64"), DestDir: dest,
			})

			if want := []string{"GET " + wantFirst, "GET https://objects.example.invalid/asset?sig=x"}; strings.Join(requests, "\n") != strings.Join(want, "\n") {
				t.Errorf("requests:\n%s\nwant:\n%s", strings.Join(requests, "\n"), strings.Join(want, "\n"))
			}

			if tc.wantErr == nil {
				if err != nil || path != filepath.Join(dest, "mise") {
					t.Fatalf("path = %q err = %v", path, err)
				}

				return
			}

			if !errors.Is(err, tc.wantErr) || path != "" {
				t.Fatalf("path = %q err = %v, want %v", path, err, tc.wantErr)
			}

			if entries, _ := filepath.Glob(filepath.Join(dest, "*")); len(entries) != 0 {
				t.Errorf("installed %v from bytes that do not match the pin", entries)
			}
		})
	}
}

// credentialedMirror is a synthetic credential on a non-resolving host.
const credentialedMirror = "https://mirror-user:mirror-secret@mirror.example.invalid/mise" //nolint:gosec // synthetic fixture credential.

// TestInstallMise_RefusesACredentialBearingBaseURL refuses a base URL with
// userinfo before any request, since the address is quoted in download errors.
func TestInstallMise_RefusesACredentialBearingBaseURL(t *testing.T) {
	t.Parallel()

	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++

		return memoryResponse(req, http.StatusOK, nil, nil), nil
	})}

	pin := strings.Repeat("a", 64)

	_, err := toolchain.InstallMise(context.Background(), client, &bytes.Buffer{}, toolchain.InstallMiseInput{
		Version: "2026.6.11", LinuxX64SHA256: pin, LinuxARM64SHA256: pin, DestDir: t.TempDir(), BaseURL: credentialedMirror,
	})
	if !errors.Is(err, errs.ErrUsage) || strings.Contains(err.Error(), "mirror-secret") || requests != 0 {
		t.Fatalf("err = %v requests = %d, want a usage error naming no credential and no request", err, requests)
	}
}
