// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/retry"
)

const defaultMiseBaseURL = "https://github.com/jdx/mise/releases/download"

const (
	miseDownloadAttempts   = 4
	miseDownloadRetryDelay = 2 * time.Second
	maxMiseArchiveBytes    = 64 << 20
	maxMiseBinaryBytes     = 64 << 20
)

// InstallMiseInput drives InstallMise.
type InstallMiseInput struct {
	Version          string
	LinuxX64SHA256   string
	LinuxARM64SHA256 string
	DestDir          string
	BaseURL          string
}

// InstallMise downloads a pinned mise release archive, verifies its SHA-256, and installs the mise binary.
func InstallMise(ctx context.Context, client *http.Client, out io.Writer, in InstallMiseInput) (string, error) {
	if err := validateInstallMiseInput(in); err != nil {
		return "", err
	}

	arch, expectedSHA, err := miseArchiveArchAndSHA(in)
	if err != nil {
		return "", err
	}

	archive, address, err := miseArchiveURL(in, arch)
	if err != nil {
		return "", err
	}

	if out != nil {
		_, _ = fmt.Fprintf(out, "Installing mise %s (%s)...\n", in.Version, arch)
	}

	body, err := downloadMiseArchive(ctx, client, address)
	if err != nil {
		return "", err
	}

	if err = verifyMiseArchiveSHA(archive, body, expectedSHA); err != nil {
		return "", err
	}

	destDir, err := resolveMiseDestDir(in.DestDir)
	if err != nil {
		return "", err
	}

	installPath, err := installMiseFromArchive(body, destDir)
	if err != nil {
		return "", err
	}

	if out != nil {
		_, _ = fmt.Fprintf(out, "mise %s installed to %s\n", in.Version, installPath)
	}

	return installPath, nil
}

// Build and validate the effective URL without constructing or sending a request.
func miseArchiveURL(in InstallMiseInput, arch string) (string, string, error) {
	baseURL := defaultString(in.BaseURL, defaultMiseBaseURL)

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid mise download URL: %w: %w", err, errs.ErrUsage)
	}

	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", "", fmt.Errorf("mise download URL must be absolute HTTP(S): %w", errs.ErrUsage)
	}

	// The address is quoted in every download error, so credentials in it
	// would end up in the job log; the pinned SHA-256 is the trust anchor, not
	// an authenticated source.
	if parsed.User != nil {
		return "", "", fmt.Errorf("mise base URL must not carry credentials: %w", errs.ErrUsage)
	}

	if strings.Contains(baseURL, "#") {
		return "", "", fmt.Errorf("mise base URL must be a directory prefix without a fragment: %w", errs.ErrUsage)
	}

	if parsed.RawQuery != "" || parsed.ForceQuery {
		return "", "", fmt.Errorf("mise base URL must be a directory prefix without a query: %w", errs.ErrUsage)
	}

	archive := fmt.Sprintf("mise-v%s-linux-%s-musl.tar.gz", in.Version, arch)
	address := strings.TrimRight(baseURL, "/") + "/v" + in.Version + "/" + archive

	return archive, address, nil
}

// resolveMiseDestDir defaults an empty destination to ~/.local/bin.
func resolveMiseDestDir(destDir string) (string, error) {
	if destDir != "" {
		return destDir, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ".local", "bin"), nil
}

func validateInstallMiseInput(in InstallMiseInput) error {
	if !exactDownloadVersion(in.Version) {
		return fmt.Errorf("mise version must be an exact MAJOR.MINOR.PATCH without a v prefix: %w", errs.ErrUsage)
	}

	if in.LinuxX64SHA256 == "" || in.LinuxARM64SHA256 == "" {
		return fmt.Errorf("both linux x64 and arm64 SHA-256 pins are required: %w", errs.ErrUsage)
	}

	for label, value := range map[string]string{"linux-x64": in.LinuxX64SHA256, "linux-arm64": in.LinuxARM64SHA256} {
		if len(value) != sha256HexLength || !isLowerHex(value) {
			return fmt.Errorf("%s SHA-256 pin must be 64 lowercase hex characters: %w", label, errs.ErrUsage)
		}
	}

	return nil
}

const sha256HexLength = 64

func isLowerHex(value string) bool {
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}

	return true
}

func miseArchiveArchAndSHA(in InstallMiseInput) (string, string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return "x64", in.LinuxX64SHA256, nil
	case "arm64":
		return "arm64", in.LinuxARM64SHA256, nil
	default:
		return "", "", fmt.Errorf("unsupported runner architecture: %s: %w", runtime.GOARCH, errs.ErrUnsupported)
	}
}

func downloadMiseArchive(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}

	return retry.Do(ctx, nil, miseDownloadAttempts, miseDownloadRetryDelay, func() ([]byte, error) {
		body, retryable, err := downloadMiseArchiveOnce(ctx, client, url)
		if err != nil && !retryable {
			return nil, retry.Permanent(err)
		}

		return body, err
	}, retry.WithBackoff(retry.Constant))
}

func downloadMiseArchiveOnce(ctx context.Context, client *http.Client, url string) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, fmt.Errorf("create request %s: %w", url, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		//nolint:errorlint // transport error is intentionally flattened: wrapping it would let a
		// mid-download cancellation match context.Canceled and change the exit-code classification.
		return nil, true, fmt.Errorf("download %s: %v: %w", url, err, errs.ErrDependencyUnavailable)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, shouldRetryMiseHTTPStatus(resp.StatusCode), fmt.Errorf("download %s: HTTP %d: %w", url, resp.StatusCode, errs.ErrDependencyUnavailable)
	}

	if resp.ContentLength > maxMiseArchiveBytes {
		return nil, false, fmt.Errorf("download %s: archive exceeds %d bytes: %w", url, maxMiseArchiveBytes, errs.ErrMalformedInput)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMiseArchiveBytes+1))
	if err != nil {
		return nil, true, fmt.Errorf("read %s: %w", url, err)
	}

	if len(body) > maxMiseArchiveBytes {
		return nil, false, fmt.Errorf("download %s: archive exceeds %d bytes: %w", url, maxMiseArchiveBytes, errs.ErrMalformedInput)
	}

	return body, false, nil
}

func shouldRetryMiseHTTPStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500
}

func verifyMiseArchiveSHA(name string, body []byte, expected string) error {
	sum := sha256.Sum256(body)

	actual := hex.EncodeToString(sum[:])
	if actual != expected {
		return fmt.Errorf("sha256 mismatch for %s: expected %s, got %s: %w", name, expected, actual, errs.ErrValidation)
	}

	return nil
}

func installMiseFromArchive(body []byte, destDir string) (string, error) {
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("read mise archive gzip: %w: %w", err, errs.ErrMalformedInput)
	}

	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return "", fmt.Errorf("read mise archive tar: %w: %w", err, errs.ErrMalformedInput)
		}

		if header.Name != "mise/bin/mise" {
			continue
		}

		// tar.Reader normalizes the legacy TypeRegA to TypeReg on read.
		if header.Typeflag != tar.TypeReg {
			return "", fmt.Errorf("mise/bin/mise is not a regular file in archive: %w", errs.ErrMalformedInput)
		}

		if header.Size < 0 || header.Size > maxMiseBinaryBytes {
			return "", fmt.Errorf("mise/bin/mise exceeds %d bytes: %w", maxMiseBinaryBytes, errs.ErrMalformedInput)
		}

		return writeMiseBinary(tr, header.Size, destDir)
	}

	return "", fmt.Errorf("mise binary not found at mise/bin/mise in archive: %w", errs.ErrMalformedInput)
}

// writeMiseBinary extracts the mise binary entry into destDir and marks it executable.
func writeMiseBinary(tr *tar.Reader, size int64, destDir string) (string, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil { //nolint:gosec // tool install dir read by later CI steps.
		return "", fmt.Errorf("create install directory %s: %w", destDir, err)
	}

	root, err := os.OpenRoot(destDir)
	if err != nil {
		return "", fmt.Errorf("open install directory %s: %w", destDir, err)
	}
	defer func() { _ = root.Close() }()

	installPath := filepath.Join(destDir, "mise")

	out, err := os.CreateTemp(destDir, ".mise-*") //nolint:gosec // destination is caller-selected and opened as an os.Root above.
	if err != nil {
		return "", fmt.Errorf("create temporary mise binary in %s: %w", destDir, err)
	}

	tempName := filepath.Base(out.Name())

	complete := false
	defer func() {
		if !complete {
			_ = out.Close()
			_ = root.Remove(tempName)
		}
	}()

	if _, err := io.CopyN(out, tr, size); err != nil {
		return "", fmt.Errorf("write %s: %w", installPath, err)
	}

	if err := out.Close(); err != nil {
		return "", fmt.Errorf("close %s: %w", installPath, err)
	}

	if err := root.Chmod(tempName, 0o755); err != nil { //nolint:gosec // mise must be executable by the runner.
		return "", fmt.Errorf("set executable mode on %s: %w", installPath, err)
	}

	if err := root.Rename(tempName, "mise"); err != nil {
		return "", fmt.Errorf("install %s: %w", installPath, err)
	}

	complete = true

	return installPath, nil
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}

	return fallback
}
