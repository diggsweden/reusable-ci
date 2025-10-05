// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// XcodeExportIPAInput drives XcodeExportIPA.
type XcodeExportIPAInput struct {
	ExportOptionsBase64 string // required; empty → error
	ExportOptionsVar    string // name of the originating env var, for error message
}

// XcodeExportIPA decodes the export-options plist from base64 and
// runs `xcodebuild -exportArchive` against build/app.xcarchive.
func XcodeExportIPA(ctx context.Context, ops XcodeBuildOps, w, stderr io.Writer, in XcodeExportIPAInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	body, err := decodeXcodeExportOptions(in)
	if err != nil {
		return err
	}

	return exportXcodeIPA(ctx, ops, w, stderr, body)
}

func decodeXcodeExportOptions(in XcodeExportIPAInput) ([]byte, error) {
	if in.ExportOptionsBase64 == "" {
		varName := in.ExportOptionsVar
		if varName == "" {
			varName = "EXPORT_OPTIONS_BASE64"
		}

		return nil, fmt.Errorf("export options not found in variable %s: %w", varName, errs.ErrMissingInput)
	}

	body, err := decodeMobileSecret(in.ExportOptionsBase64)
	if err != nil {
		return nil, fmt.Errorf("decode export options: %w: %w", err, errs.ErrMalformedInput)
	}

	return body, nil
}

func exportXcodeIPA(ctx context.Context, ops XcodeBuildOps, w, stderr io.Writer, body []byte) (err error) { //nolint:varnamelen // writer convention.
	tempDir, err := mobileTempRoot()
	if err != nil {
		return err
	}

	stage, err := pathsafe.NewArtifactStaging(tempDir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, stage.Close()) }()

	if writeErr := stage.Root().WriteFile("export-options.plist", body, 0o600); writeErr != nil {
		return fmt.Errorf("write export-options.plist: %w", writeErr)
	}

	code, err := ops.RunInherit(ctx, w, stderr,
		"-exportArchive",
		"-archivePath", "build/app.xcarchive",
		"-exportPath", "build/export",
		"-exportOptionsPlist", filepath.Join(stage.Root().Name(), "export-options.plist"),
	)
	if err != nil {
		return fmt.Errorf("xcodebuild -exportArchive: %w", err)
	}

	if code != 0 {
		return fmt.Errorf("xcodebuild -exportArchive exited with status %d: %w", code, errs.ErrDependencyUnavailable)
	}

	return nil
}
