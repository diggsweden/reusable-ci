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

func TestXcodeListBuiltArtifacts(t *testing.T) {
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

// TestXcodeListBuiltArtifacts_SkipsArchiveBundles records a real gap.
//
// xcodebuild produces .xcarchive as a bundle *directory*, and the walk
// returns early on every directory, so an archive is never listed. The
// previous fixture wrote build/app.xcarchive as a plain file -- which
// xcodebuild never produces -- and the test passed on that basis alone.
//
// It matters most on the unsigned path, whose only output is the archive:
// that build reports "No artifacts found" for a build that succeeded. See
// docs/open-questions.md.
func TestXcodeListBuiltArtifacts_SkipsArchiveBundles(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	// The shape xcodebuild actually writes.
	fsys.WriteFile(filepath.Join("build", "app.xcarchive", "Info.plist"), []byte("<plist/>"))
	fsys.WriteFile(filepath.Join("build", "app.xcarchive", "Products", "Applications", "app.app", "app"), []byte("bin"))

	var out bytes.Buffer

	if err := appbuild.XcodeListBuiltArtifacts(&out); err != nil {
		t.Fatal(err)
	}

	if got, want := out.String(), "Built artifacts:\nNo artifacts found\n"; got != want {
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
