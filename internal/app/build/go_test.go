// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

type fakeGoTool struct{ calls []appbuild.GoRunInput }

func (f *fakeGoTool) Run(_ context.Context, in appbuild.GoRunInput) error {
	f.calls = append(f.calls, in)

	return nil
}

type fakeCycloneDXGoModTool struct{ calls []appbuild.GoRunInput }

func (f *fakeCycloneDXGoModTool) Run(_ context.Context, in appbuild.GoRunInput) error {
	f.calls = append(f.calls, in)

	return nil
}

func TestGoMetadata_EmitsOutputs(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("go.mod", []byte("module github.com/org/app\n"))

	sink := fakeoutputsink.New(t)

	var out bytes.Buffer

	if err := appbuild.GoMetadata(context.Background(), sink, &out, appbuild.GoMetadataInput{Dir: fsys.Root, RefName: "v1.2.3"}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Fatal(err)
	}

	if got := sink.Single("binary-name"); got != "app" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("binary-name = %q", got)
	}

	if got := sink.Single("version"); got != "1.2.3" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("version = %q", got)
	}

	if got := sink.Single("module"); got != "github.com/org/app" {
		t.Errorf("module = %q", got)
	}

	if !strings.Contains(out.String(), "Binary: app") {
		t.Errorf("out = %s", out.String())
	}
}

func TestGoMetadata_UsesExplicitBinaryAndVersion(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("go.mod", []byte("module github.com/org/app\n"))

	sink := fakeoutputsink.New(t)

	if err := appbuild.GoMetadata(context.Background(), sink, &bytes.Buffer{}, appbuild.GoMetadataInput{Dir: fsys.Root, ArtifactName: "artifact", BinaryNameInput: "bin", VersionInput: "2.0.0", RefName: "v1.2.3"}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Fatal(err)
	}

	if got := sink.Single("binary-name"); got != "bin" {
		t.Errorf("binary-name = %q", got)
	}

	if got := sink.Single("version"); got != "2.0.0" {
		t.Errorf("version = %q", got)
	}
}

func TestGoMetadata_RejectsMissingModule(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("go.mod", []byte("go 1.26\n"))

	sink := fakeoutputsink.New(t)

	err := appbuild.GoMetadata(context.Background(), sink, &bytes.Buffer{}, appbuild.GoMetadataInput{Dir: fsys.Root})
	if err == nil || !strings.Contains(err.Error(), "module directive") {
		t.Fatalf("err = %v", err)
	}
}

func TestGoTest_RunsWithTags(t *testing.T) {
	t.Parallel()

	tool := &fakeGoTool{}
	if err := appbuild.GoTest(context.Background(), tool, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.GoTestInput{Dir: "src", BuildTags: "integration"}); err != nil {
		t.Fatal(err)
	}

	if got := strings.Join(tool.calls[0].Args, " "); got != "test -tags integration ./..." {
		t.Errorf("args = %q", got)
	}
}

func TestGoDownload_RunsModDownload(t *testing.T) {
	t.Parallel()

	tool := &fakeGoTool{}
	if err := appbuild.GoDownload(context.Background(), tool, &bytes.Buffer{}, &bytes.Buffer{}, "src"); err != nil {
		t.Fatal(err)
	}

	if got := strings.Join(tool.calls[0].Args, " "); got != "mod download" {
		t.Errorf("args = %q", got)
	}
}

func TestGoBuildSBOM_WritesCanonicalPath(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)

	tool := &fakeCycloneDXGoModTool{}
	if err := appbuild.GoBuildSBOM(context.Background(), tool, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.GoBuildSBOMInput{
		Dir:        fsys.Root,
		BinaryName: "app",
	}); err != nil {
		t.Fatal(err)
	}

	if len(tool.calls) != 1 {
		t.Fatalf("calls = %+v", tool.calls)
	}

	if got := strings.Join(tool.calls[0].Args, " "); !strings.Contains(got, filepath.Join(".reusable-ci", "go-build-sbom", "app", "bom.json")) {
		t.Errorf("args = %q", got)
	}

	if _, err := os.Stat(filepath.Join(fsys.Root, ".reusable-ci", "go-build-sbom", "app")); err != nil {
		t.Fatalf("sbom dir missing: %v", err)
	}
}

func TestGoBuildBinaries_BuildsPlatforms(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)

	fysDist := fsys.WriteFile("dist/old", []byte("old"))
	if fysDist == "" {
		t.Fatal("fixture not written")
	}

	tool := &fakeGoTool{}

	var out bytes.Buffer
	if err := appbuild.GoBuildBinaries(context.Background(), tool, &out, &bytes.Buffer{}, appbuild.GoBuildBinariesInput{
		Dir:         fsys.Root,
		BinaryName:  "app",
		BuildTags:   "netgo",
		LDFlags:     "-X main.extra=value",
		MainPackage: "./cmd/app",
		Platforms:   "linux/amd64, windows/arm64",
		Version:     "1.2.3",
		Commit:      "abc123",
	}); err != nil {
		t.Fatal(err)
	}

	if len(tool.calls) != 2 {
		t.Fatalf("calls = %+v", tool.calls)
	}

	if got := strings.Join(tool.calls[0].Env, " "); got != "CGO_ENABLED=0 GOOS=linux GOARCH=amd64" {
		t.Errorf("env = %q", got)
	}

	if got := strings.Join(tool.calls[1].Args, " "); !strings.Contains(got, filepath.Join("dist", "windows-arm64", "app-windows-arm64.exe")) {
		t.Errorf("windows args = %q", got)
	}

	if !strings.Contains(out.String(), "Building linux/amd64") {
		t.Errorf("out = %s", out.String())
	}
}

func TestGoBuildBinaries_RejectsInvalidPlatformBeforeRemovingDist(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	old := fsys.WriteFile("dist/old", []byte("old"))

	err := appbuild.GoBuildBinaries(context.Background(), &fakeGoTool{}, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.GoBuildBinariesInput{
		Dir:        fsys.Root,
		BinaryName: "app",
		Version:    "v1.2.3",
		Platforms:  "linux/amd64/v2",
	})
	if err == nil || !strings.Contains(err.Error(), "expected GOOS/GOARCH") {
		t.Fatalf("err = %v", err)
	}

	if _, statErr := os.Stat(old); statErr != nil {
		t.Fatalf("dist was removed before validation: %v", statErr)
	}
}
