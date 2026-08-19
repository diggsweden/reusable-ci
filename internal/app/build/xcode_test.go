// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestXcodeArtifactName_EmitsNameWithTag(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	var out bytes.Buffer

	if err := appbuild.XcodeArtifactName(context.Background(), sink, &out, appbuild.XcodeArtifactNameInput{RepositoryName: "demo", IncludeTag: true, RefName: "v1.2.3"}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Fatal(err)
	}

	if got := sink.Single("ipa-name"); got != "demo-v1.2.3" {
		t.Errorf("ipa-name = %q", got)
	}

	if !strings.Contains(out.String(), "IPA artifact: demo-v1.2.3") {
		t.Errorf("out = %s", out.String())
	}
}

func TestXcodeArtifactName_IncludeTagWithEmptyRefKeepsBaseName(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	if err := appbuild.XcodeArtifactName(context.Background(), sink, io.Discard, appbuild.XcodeArtifactNameInput{RepositoryName: "demo", IncludeTag: true}); err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("ipa-name"); got != "demo" {
		t.Errorf("ipa-name = %q", got)
	}
}

func TestXcodeXCConfig_DecodesAndEmitsPath(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	sink := fakeoutputsink.New(t)

	var stderr bytes.Buffer

	body := []byte("SETTING = value\n")

	if err := appbuild.XcodeXCConfig(context.Background(), sink, output.NewAnnotator(&stderr, output.FormatGitHub), appbuild.XcodeXCConfigInput{
		Base64:  base64.StdEncoding.EncodeToString(body),
		TempDir: fsys.Root,
	}); err != nil {
		t.Fatal(err)
	}

	path := sink.Single("xcconfig-path")
	if !strings.HasPrefix(path, filepath.Join(fsys.Root, "ci-")) || !strings.HasSuffix(path, ".xcconfig") {
		t.Errorf("xcconfig-path = %q", path)
	}

	got, err := os.ReadFile(path) //nolint:gosec // test fixture; path is sink output.
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, body) {
		t.Errorf("body = %q", got)
	}

	// The xcconfig is a secret -- it carries build settings including
	// signing identity and team id -- and lands in a shared temp dir.
	// os.CreateTemp happens to make it 0600; asserting it keeps that true
	// if the write is ever done another way.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("xcconfig mode = %v, want 0600", perm)
	}

	if !strings.Contains(stderr.String(), "xcconfig decoded") {
		t.Errorf("stderr = %s", stderr.String())
	}
}

func TestXcodeXCConfig_MissingSecretNoops(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	var stderr bytes.Buffer

	if err := appbuild.XcodeXCConfig(context.Background(), sink, output.NewAnnotator(&stderr, output.FormatGitHub), appbuild.XcodeXCConfigInput{}); err != nil {
		t.Fatal(err)
	}

	// Keys, not Single: an absent key and a key set to "" both read as ""
	// through Single, and a workflow handed an empty xcconfig-path behaves
	// differently from one handed none.
	if got := sink.Keys(); len(got) != 0 {
		t.Errorf("emitted %q for a run with no xcconfig", got)
	}

	if !strings.Contains(stderr.String(), "no xcconfig") {
		t.Errorf("stderr = %s", stderr.String())
	}
}

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

	if got := sink.Single("version"); got != "1.2.3" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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

	if got := sink.Single("version"); got != "9.9.9" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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

	if got := sink.Single("version"); got != "unknown" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("version = %q, want unknown", got)
	}

	if got := sink.Single("build"); got != "unknown" {
		t.Errorf("build = %q, want unknown", got)
	}

	if !strings.Contains(stderr.String(), "::warning::Could not determine version from project file") {
		t.Errorf("missing warning in stderr:\n%s", stderr.String())
	}
}
