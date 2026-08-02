// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/app/toolchain"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestInstallMise_DownloadsVerifiesAndInstalls(t *testing.T) {
	t.Parallel()

	archive := miseArchive(t, "#!/bin/sh\necho mise\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, expectedArchivePath()) {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}

		_, _ = w.Write(archive)
	}))
	t.Cleanup(server.Close)

	dest := t.TempDir()

	var out bytes.Buffer

	installPath, err := toolchain.InstallMise(context.Background(), server.Client(), &out, toolchain.InstallMiseInput{
		Version:          "2026.6.11",
		LinuxX64SHA256:   shaHexForArch(archive, "amd64"),
		LinuxARM64SHA256: shaHexForArch(archive, "arm64"),
		DestDir:          dest,
		BaseURL:          server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}

	if installPath != filepath.Join(dest, "mise") {
		t.Fatalf("installPath = %q", installPath)
	}

	body, err := os.ReadFile(installPath) //nolint:gosec // test fixture path.
	if err != nil {
		t.Fatal(err)
	}

	if string(body) != "#!/bin/sh\necho mise\n" {
		t.Fatalf("installed body = %q", string(body))
	}

	info, err := os.Stat(installPath)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %o, want 755", info.Mode().Perm())
	}

	if !strings.Contains(out.String(), "Installing mise 2026.6.11") || !strings.Contains(out.String(), "installed to") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestInstallMise_RejectsChecksumMismatch(t *testing.T) {
	t.Parallel()

	archive := miseArchive(t, "mise")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(archive) }))
	t.Cleanup(server.Close)

	_, err := toolchain.InstallMise(context.Background(), server.Client(), nil, toolchain.InstallMiseInput{
		Version:          "2026.6.11",
		LinuxX64SHA256:   strings.Repeat("0", 64),
		LinuxARM64SHA256: strings.Repeat("0", 64),
		DestDir:          t.TempDir(),
		BaseURL:          server.URL,
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

func TestInstallMise_RejectsArchiveWithoutBinary(t *testing.T) {
	t.Parallel()

	archive := tarGzip(t, map[string]string{"mise/README.md": "no binary"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(archive) }))
	t.Cleanup(server.Close)

	_, err := toolchain.InstallMise(context.Background(), server.Client(), nil, toolchain.InstallMiseInput{
		Version:          "2026.6.11",
		LinuxX64SHA256:   shaHexForArch(archive, "amd64"),
		LinuxARM64SHA256: shaHexForArch(archive, "arm64"),
		DestDir:          t.TempDir(),
		BaseURL:          server.URL,
	})
	if !errors.Is(err, errs.ErrMalformedInput) {
		t.Fatalf("err = %v, want ErrMalformedInput", err)
	}
}

func TestInstallMise_RejectsInvalidPinsBeforeDownload(t *testing.T) {
	t.Parallel()

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	t.Cleanup(server.Close)

	_, err := toolchain.InstallMise(context.Background(), server.Client(), nil, toolchain.InstallMiseInput{
		Version:          "2026.6.11",
		LinuxX64SHA256:   "bad",
		LinuxARM64SHA256: strings.Repeat("0", 64),
		DestDir:          t.TempDir(),
		BaseURL:          server.URL,
	})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	if called {
		t.Fatal("download started despite invalid checksum pin")
	}
}

func miseArchive(t *testing.T, body string) []byte {
	t.Helper()

	return tarGzip(t, map[string]string{"mise/bin/mise": body})
}

func tarGzip(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)

	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}

		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func shaHexForArch(body []byte, arch string) string {
	if runtime.GOARCH != arch {
		return strings.Repeat("0", 64)
	}

	sum := sha256.Sum256(body)

	return hex.EncodeToString(sum[:])
}

func expectedArchivePath() string {
	arch := "x64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}

	return "/v2026.6.11/mise-v2026.6.11-linux-" + arch + "-musl.tar.gz"
}
