// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package publish

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	domainpublish "github.com/diggsweden/reusable-ci/internal/domain/publish"
)

// AppStorePrepareCredentialsInput drives PrepareAppStoreCredentials.
// The CLI layer is responsible for sourcing the secrets (typically from
// env vars like APP_STORE_CONNECT_API_KEY_ID and
// APP_STORE_CONNECT_API_PRIVATE_KEY_BASE64) so this layer is free of
// I/O and trivially testable.
type AppStorePrepareCredentialsInput struct {
	// Dir is the destination directory for the decoded key. Defaults to
	// "private_keys" — matching `xcrun altool`'s default lookup path.
	Dir string
	// KeyID is the App Store Connect API key ID. Required.
	KeyID string
	// PrivateKeyB64 is the base64-encoded private key body. Whitespace
	// in the payload is tolerated (multi-line secrets pasted via
	// GitHub's secret UI sometimes carry trailing newlines). Required.
	PrivateKeyB64 string
}

// PrepareAppStoreCredentials decodes the App Store Connect API private key
// from a base64 secret and writes it to `<Dir>/AuthKey_<KEY_ID>.p8` with mode
// 0600 — what xcrun altool expects.
func PrepareAppStoreCredentials(_ context.Context, w io.Writer, in AppStorePrepareCredentialsInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if in.KeyID == "" {
		return fmt.Errorf("app store API key ID is required: %w", errs.ErrMissingInput)
	}

	if !domainpublish.IsAppStoreKeyID(in.KeyID) {
		return fmt.Errorf("app store API key ID is not a valid value: %w", errs.ErrValidation)
	}

	if in.PrivateKeyB64 == "" {
		return fmt.Errorf("app store API private key is required: %w", errs.ErrMissingInput)
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(in.PrivateKeyB64), ""))
	if err != nil {
		return fmt.Errorf("decode App Store API private key: %w: %w", err, errs.ErrValidation)
	}

	if len(decoded) == 0 {
		return fmt.Errorf("app store API private key decoded to zero bytes: %w", errs.ErrValidation)
	}

	dir := in.Dir
	if dir == "" {
		dir = "private_keys"
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	path := filepath.Join(dir, "AuthKey_"+in.KeyID+".p8")
	if err := os.WriteFile(path, decoded, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	_, _ = fmt.Fprintf(w, "✓ App Store Connect API configured\n")

	return nil
}

// AppStoreUploadResultInput drives EmitAppStoreUploadResult.
type AppStoreUploadResultInput struct {
	Path string
}

// EmitAppStoreUploadResult reads altool's upload-result JSON, emits
// request-id when present, and mirrors the old workflow behavior of doing
// nothing when the file is absent.
func EmitAppStoreUploadResult(ctx context.Context, sink ci.OutputSink, w io.Writer, in AppStoreUploadResultInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	path := in.Path
	if path == "" {
		path = "upload-result.json"
	}

	body, err := os.ReadFile(path) //nolint:gosec // path is CLI-flag-derived.
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("read App Store upload result: %w", err)
	}

	requestID, err := domainpublish.ParseAppStoreUploadRequestID(body)
	if err != nil {
		return err
	}

	if err := sink.Set(ctx, "request-id", requestID); err != nil {
		return fmt.Errorf("set request-id: %w", err)
	}

	_, _ = fmt.Fprintf(w, "Upload Request ID: %s\n", requestID)

	return nil
}
