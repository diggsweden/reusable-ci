// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// XcodeExportIPAInput drives XcodeExportIPA.
type XcodeExportIPAInput struct {
	ExportOptionsBase64 string // required; empty → error
	ExportOptionsVar    string // name of the originating env var, for error message
}

// XcodeExportIPA decodes the export-options plist from base64 and
// runs `xcodebuild -exportArchive` against build/app.xcarchive.
func XcodeExportIPA(ctx context.Context, ops XcodeBuildOps, w, stderr io.Writer, in XcodeExportIPAInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if in.ExportOptionsBase64 == "" {
		varName := in.ExportOptionsVar
		if varName == "" {
			varName = "EXPORT_OPTIONS_BASE64"
		}

		return fmt.Errorf("export options not found in variable %s: %w", varName, errs.ErrMissingInput)
	}

	clean := strings.Map(func(r rune) rune { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		switch r {
		case ' ', '\t', '\r', '\n':
			return -1
		}

		return r
	}, in.ExportOptionsBase64)

	body, err := base64.StdEncoding.DecodeString(clean)
	if err != nil {
		return fmt.Errorf("decode export options: %w: %w", err, errs.ErrMalformedInput)
	}

	if writeErr := os.WriteFile("export-options.plist", body, 0o600); writeErr != nil {
		return fmt.Errorf("write export-options.plist: %w", writeErr)
	}

	code, err := ops.RunInherit(ctx, w, stderr,
		"-exportArchive",
		"-archivePath", "build/app.xcarchive",
		"-exportPath", "build/export",
		"-exportOptionsPlist", "export-options.plist",
	)
	if err != nil {
		return fmt.Errorf("xcodebuild -exportArchive: %w", err)
	}

	if code != 0 {
		return fmt.Errorf("xcodebuild -exportArchive exited with status %d: %w", code, errs.ErrDependencyUnavailable)
	}

	return nil
}
