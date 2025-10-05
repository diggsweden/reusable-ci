// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"path/filepath"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestXcodeListBuiltArtifacts_ListsOnlyTheExportedIPA(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile(filepath.Join("build", "export", "Demo.ipa"), []byte("fake-ipa"))
	fsys.WriteFile(filepath.Join("build", "junk.txt"), []byte("ignore"))

	var out bytes.Buffer

	if err := appbuild.XcodeListBuiltArtifacts(&out); err != nil {
		t.Fatal(err)
	}

	want := "Built artifacts:\n" + filepath.Join("build", "export", "Demo.ipa") + "\n"
	if got := out.String(); got != want {
		t.Errorf("listing = %q, want %q", got, want)
	}
}

// TestXcodeListBuiltArtifacts_ListsArchiveBundles uses the directory shape
// xcodebuild actually writes rather than an impossible plain .xcarchive file.
func TestXcodeListBuiltArtifacts_ListsArchiveBundles(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	// The shape xcodebuild actually writes.
	fsys.WriteFile(filepath.Join("build", "app.xcarchive", "Info.plist"), []byte("<plist/>"))
	fsys.WriteFile(filepath.Join("build", "app.xcarchive", "Products", "Applications", "app.app", "app"), []byte("bin"))

	var out bytes.Buffer

	if err := appbuild.XcodeListBuiltArtifacts(&out); err != nil {
		t.Fatal(err)
	}

	if got, want := out.String(), "Built artifacts:\n"+filepath.Join("build", "app.xcarchive")+"\n"; got != want {
		t.Errorf("listing = %q, want %q", got, want)
	}
}

func TestXcodeListBuiltArtifacts_MissingBuildDir(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	var out bytes.Buffer

	if err := appbuild.XcodeListBuiltArtifacts(&out); err != nil {
		t.Fatal(err)
	}

	if got, want := out.String(), "Built artifacts:\nNo artifacts found\n"; got != want {
		t.Errorf("listing = %q, want %q", got, want)
	}
}
