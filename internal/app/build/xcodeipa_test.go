// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build_test

import (
	"context"
	"encoding/base64"
	"io"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
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
