// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
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

	if body := fsys.ReadFile("export-options.plist"); string(body) != plist {
		t.Errorf("plist = %q", body)
	}

	// The plist carries the signing configuration -- team id, provisioning
	// profile, distribution method -- and is written into the workspace, so
	// it is owner-only by intent.
	info, err := os.Stat(filepath.Join(fsys.Root, "export-options.plist"))
	if err != nil {
		t.Fatal(err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("plist mode = %v, want 0600", perm)
	}

	// The whole invocation. Reading only calls[0][0] left the archive it
	// exports, the directory it exports to, and the plist it was just told
	// to write all unasserted -- so the three paths could disagree and the
	// test would not know.
	want := []string{
		"-exportArchive",
		"-archivePath", filepath.Join("build", "app.xcarchive"),
		"-exportPath", filepath.Join("build", "export"),
		"-exportOptionsPlist", "export-options.plist",
	}

	if len(ops.calls) != 1 {
		t.Fatalf("xcodebuild calls = %v, want 1", ops.calls)
	}

	if !reflect.DeepEqual(ops.calls[0], want) {
		t.Errorf("export = %q, want %q", ops.calls[0], want)
	}
}

// TestXcodeExportIPA_MissingOptionsNamesItsVariable covers the refusal.
// The error names the env var the operator has to set, taken from the
// caller rather than hard-coded, so a workflow using its own name gets
// told that name.
func TestXcodeExportIPA_MissingOptionsNamesItsVariable(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		varName string
		want    string
	}{
		{
			name:    "caller-supplied name",
			varName: "IOS_EXPORT_OPTIONS",
			want:    "export options not found in variable IOS_EXPORT_OPTIONS",
		},
		{
			name: "no name falls back to the documented default",
			want: "export options not found in variable EXPORT_OPTIONS_BASE64",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ops := &fakeXcodeBuild{}

			err := appbuild.XcodeExportIPA(context.Background(), ops, io.Discard, io.Discard, appbuild.XcodeExportIPAInput{
				ExportOptionsVar: tc.varName,
			})
			if !errors.Is(err, errs.ErrMissingInput) {
				t.Fatalf("err = %v, want ErrMissingInput", err)
			}

			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to name %q", err, tc.want)
			}

			if len(ops.calls) != 0 {
				t.Errorf("ran xcodebuild without export options: %v", ops.calls)
			}
		})
	}
}
