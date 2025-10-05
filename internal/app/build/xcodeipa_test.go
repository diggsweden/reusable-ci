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
	"slices"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func xcodeExportFixture(t *testing.T) *testfs.Real {
	t.Helper()
	fsys := testfs.NewReal(t)

	root, err := filepath.EvalSymlinks(fsys.Root)
	if err != nil {
		t.Fatal(err)
	}

	fsys.Root = root
	for _, key := range []string{"TMPDIR", "TEMP", "TMP"} {
		t.Setenv(key, fsys.MkdirAll(key))
	}

	return fsys
}

func TestXcodeExportIPA_WritesPlistAndInvokesXcodebuild(t *testing.T) {
	fsys := xcodeExportFixture(t)
	fsys.Chdir()

	plist := `<?xml version="1.0"?>`
	enc := base64.StdEncoding.EncodeToString([]byte(plist))

	fsys.WriteFile("export-options.plist", []byte("workspace canary"))

	var plistPath string

	ops := &fakeXcodeBuild{run: func(args []string) {
		if len(args) != 7 {
			t.Fatalf("args=%v", args)
		}

		plistPath = args[6]
		if filepath.Dir(filepath.Dir(plistPath)) != fsys.Path("TMPDIR") {
			t.Fatalf("export plist escaped fixture temp root: %s", plistPath)
		}

		body, err := os.ReadFile(plistPath)
		if err != nil || string(body) != plist {
			t.Fatalf("body=%q err=%v", body, err)
		}

		info, err := os.Lstat(plistPath)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			t.Fatalf("plist info=%v err=%v", info, err)
		}
	}}
	if err := appbuild.XcodeExportIPA(context.Background(), ops, io.Discard, io.Discard, appbuild.XcodeExportIPAInput{
		ExportOptionsBase64: enc,
		ExportOptionsVar:    "EXPORT_OPTIONS_BASE64",
	}); err != nil {
		t.Fatal(err)
	}

	if body := fsys.ReadFile("export-options.plist"); string(body) != "workspace canary" {
		t.Errorf("plist = %q", body)
	}

	if _, err := os.Stat(plistPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("transient plist remains: %v", err)
	}

	// The whole invocation. Reading only calls[0][0] left the archive it
	// exports, the directory it exports to, and the plist it was just told
	// to write all unasserted -- so the three paths could disagree and the
	// test would not know.
	want := []string{
		"-exportArchive",
		"-archivePath", filepath.Join("build", "app.xcarchive"),
		"-exportPath", filepath.Join("build", "export"),
		"-exportOptionsPlist", plistPath,
	}

	if len(ops.calls) != 1 {
		t.Fatalf("xcodebuild calls = %v, want 1", ops.calls)
	}

	if !slices.Equal(ops.calls[0], want) {
		t.Errorf("export = %q, want %q", ops.calls[0], want)
	}
}

// TestXcodeExportIPA_MissingOptionsNamesItsVariable covers the refusal.
// The error names the env var the operator has to set, taken from the
// caller rather than hard-coded, so a workflow using its own name gets
// told that name.
func TestXcodeExportIPA_MissingOptionsNamesItsVariable(t *testing.T) {
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
			xcodeExportFixture(t)

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
