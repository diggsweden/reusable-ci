// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestXcodeExportIPA_WritesPlistAndInvokesXcodebuild(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	plist := `<?xml version="1.0"?>`
	enc := base64.StdEncoding.EncodeToString([]byte(plist))

	ops := &fakeXcodeBuild{}
	if err := appbuild.XcodeExportIPA(context.Background(), ops, io.Discard, io.Discard, appbuild.XcodeExportIPAInput{
		ExportOptionsBase64: enc,
		ExportOptionsVar:    "EXPORT_OPTIONS_BASE64",
	}); err != nil {
		t.Fatal(err)
	}

	body := fsys.ReadFile("export-options.plist")
	if string(body) != plist {
		t.Errorf("plist = %q", body)
	}

	if len(ops.calls) != 1 || ops.calls[0][0] != "-exportArchive" {
		t.Errorf("xcodebuild call = %v", ops.calls)
	}
}

func TestXcodeExportIPA_EmptyOptionsErrors(t *testing.T) {
	err := appbuild.XcodeExportIPA(context.Background(), &fakeXcodeBuild{}, io.Discard, io.Discard, appbuild.XcodeExportIPAInput{
		ExportOptionsVar: "EXPORT_OPTIONS_BASE64",
	})
	if err == nil || !strings.Contains(err.Error(), "EXPORT_OPTIONS_BASE64") {
		t.Errorf("expected env-name error, got: %v", err)
	}
}

// The missing-options error names the originating env var so the operator
// knows which one to set — using the caller-supplied ExportOptionsVar, not a
// hard-coded default. (Previously asserted via the now-pruned `build xcode-ios
// export-ipa` CLI command; pinned here at the app layer.)
func TestXcodeExportIPA_EmptyOptions_ReportsCustomVarName(t *testing.T) {
	err := appbuild.XcodeExportIPA(context.Background(), &fakeXcodeBuild{}, io.Discard, io.Discard, appbuild.XcodeExportIPAInput{
		ExportOptionsVar: "IOS_EXPORT_OPTIONS",
	})

	if err == nil || !strings.Contains(err.Error(), "export options not found in variable IOS_EXPORT_OPTIONS") {
		t.Errorf("error must name the custom var IOS_EXPORT_OPTIONS, got: %v", err)
	}

	if !errors.Is(err, errs.ErrMissingInput) {
		t.Errorf("missing export options must wrap ErrMissingInput, got: %v", err)
	}
}

// An empty ExportOptionsVar falls back to the default env-var name in the error.
func TestXcodeExportIPA_EmptyOptions_DefaultsVarNameWhenUnset(t *testing.T) {
	err := appbuild.XcodeExportIPA(context.Background(), &fakeXcodeBuild{}, io.Discard, io.Discard, appbuild.XcodeExportIPAInput{})
	if err == nil || !strings.Contains(err.Error(), "EXPORT_OPTIONS_BASE64") {
		t.Errorf("empty ExportOptionsVar should default to EXPORT_OPTIONS_BASE64, got: %v", err)
	}
}
