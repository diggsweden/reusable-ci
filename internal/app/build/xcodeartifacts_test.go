// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestXcodeListBuiltArtifacts(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile(filepath.Join("build", "export", "Demo.ipa"), []byte("fake-ipa"))
	fsys.WriteFile(filepath.Join("build", "app.xcarchive"), []byte("fake-arch"))
	fsys.WriteFile(filepath.Join("build", "junk.txt"), []byte("ignore"))

	var out bytes.Buffer
	if err := appbuild.XcodeListBuiltArtifacts(&out); err != nil {
		t.Fatal(err)
	}

	got := out.String()
	if !strings.Contains(got, "Built artifacts:") {
		t.Errorf("missing header in:\n%s", got)
	}

	for _, want := range []string{"Demo.ipa", "app.xcarchive"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}

	if strings.Contains(got, "junk.txt") {
		t.Errorf("unexpected non-artifact file in listing:\n%s", got)
	}
}

func TestXcodeListBuiltArtifacts_MissingBuildDir(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	var out bytes.Buffer
	if err := appbuild.XcodeListBuiltArtifacts(&out); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "No artifacts found") {
		t.Errorf("missing notice:\n%s", out.String())
	}
}
