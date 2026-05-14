// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build_test

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

// makeXcodeProject writes a minimal .xcodeproj/project.pbxproj under the
// provided testfs root containing the given key/value pairs.
func makeXcodeProject(t *testing.T, fsys *testfs.Real, name, version, build string) string {
	t.Helper()
	dir := fsys.Path(name + ".xcodeproj")
	body := "MARKETING_VERSION = " + version + ";\nCURRENT_PROJECT_VERSION = " + build + ";\n"
	fsys.WriteFile(filepath.Join(name+".xcodeproj", "project.pbxproj"), []byte(body))
	return dir
}

func TestXcodeVersionInfo_ExplicitProject(t *testing.T) {
	fsys := testfs.NewReal(t)
	root := fsys.Root
	proj := makeXcodeProject(t, fsys, "Demo", "1.2.3", "42")
	sink := fakeoutputsink.New(t)
	err := appbuild.XcodeVersionInfo(context.Background(), sink, io.Discard, output.Annotator{}, appbuild.XcodeVersionInfoInput{
		Project: proj,
		Root:    root,
	})
	if err != nil {
		t.Fatalf("XcodeVersionInfo: %v", err)
	}
	if got := sink.Single("version"); got != "1.2.3" {
		t.Errorf("version = %q", got)
	}
	if got := sink.Single("build"); got != "42" {
		t.Errorf("build = %q", got)
	}
}

func TestXcodeVersionInfo_PrintsStderrLine(t *testing.T) {
	fsys := testfs.NewReal(t)
	root := fsys.Root
	proj := makeXcodeProject(t, fsys, "Demo", "2.1.0", "15")
	sink := fakeoutputsink.New(t)
	var stderr bytes.Buffer
	err := appbuild.XcodeVersionInfo(context.Background(), sink, &stderr, output.Annotator{}, appbuild.XcodeVersionInfoInput{
		Project: proj,
		Root:    root,
	})
	if err != nil {
		t.Fatalf("XcodeVersionInfo: %v", err)
	}
	if got := strings.TrimSpace(stderr.String()); got != "Version: 2.1.0 (15)" {
		t.Errorf("stderr = %q", got)
	}
}

func TestXcodeVersionInfo_AutoDiscoversXcodeproj(t *testing.T) {
	fsys := testfs.NewReal(t)
	root := fsys.Root
	makeXcodeProject(t, fsys, "Demo", "9.9.9", "100")
	sink := fakeoutputsink.New(t)
	if err := appbuild.XcodeVersionInfo(context.Background(), sink, io.Discard, output.Annotator{}, appbuild.XcodeVersionInfoInput{
		Root: root,
	}); err != nil {
		t.Fatalf("XcodeVersionInfo: %v", err)
	}
	if got := sink.Single("version"); got != "9.9.9" {
		t.Errorf("version = %q", got)
	}
}

func TestXcodeVersionInfo_NoProjectFallsBackToUnknown(t *testing.T) {
	fsys := testfs.NewReal(t)
	root := fsys.Root // no .xcodeproj
	sink := fakeoutputsink.New(t)
	var stderr bytes.Buffer
	if err := appbuild.XcodeVersionInfo(context.Background(), sink, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appbuild.XcodeVersionInfoInput{
		Root: root,
	}); err != nil {
		t.Fatalf("XcodeVersionInfo: %v", err)
	}
	if got := sink.Single("version"); got != "unknown" {
		t.Errorf("version = %q, want unknown", got)
	}
	if got := sink.Single("build"); got != "unknown" {
		t.Errorf("build = %q, want unknown", got)
	}
	if !strings.Contains(stderr.String(), "::warning::Could not determine version from project file") {
		t.Errorf("missing warning in stderr:\n%s", stderr.String())
	}
}
