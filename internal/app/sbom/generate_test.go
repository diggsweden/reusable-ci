// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	appsbom "github.com/diggsweden/reusable-ci/v3/internal/app/sbom"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	domainsbom "github.com/diggsweden/reusable-ci/v3/internal/domain/sbom"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
	"github.com/stretchr/testify/require"
)

// fakeSyft writes a deterministic body to each requested output file.
// Each Generate call records one entry — when multiple formats are
// requested in one call they all share that call entry, and one body
// is written per (target, format) pair.
type fakeSyft struct {
	calls []syftCall
	err   error
}

type syftCall struct {
	target  string
	outputs map[string]string
}

func (f *fakeSyft) Generate(_ context.Context, target string, outputs map[string]string, _ io.Writer) error {
	// Defensive copy so caller mutations don't affect the recorded call.
	recorded := make(map[string]string, len(outputs))
	for k, v := range outputs {
		recorded[k] = v
	}

	f.calls = append(f.calls, syftCall{target: target, outputs: recorded})
	if f.err != nil {
		return f.err
	}

	for format, outputFile := range outputs {
		body := []byte(`{"target":"` + target + `","format":"` + format + `"}`)
		if err := os.WriteFile(outputFile, body, 0o644); err != nil { //nolint:gosec // test fixture
			return err
		}
	}

	return nil
}

type fakeMaven struct {
	answers map[string]string
}

func (f *fakeMaven) EvalExpression(_ context.Context, expr string) (string, error) {
	if f.answers == nil {
		return "", errors.New("no answers configured") //nolint:err113 // test mock error
	}

	v, ok := f.answers[expr]
	if !ok {
		return "", errors.New("unknown expr") //nolint:err113 // test mock error
	}

	return v, nil
}

type fakeGit struct {
	sha string
}

// These two stand in for an injected failure the test then matches with
// errors.Is. Package-level so the assertion has a stable identity to match --
// the thing a dynamically built error cannot give it.
var (
	errWalkFailed               = errors.New("read directory failed")
	errBuildSBOMGeneratorFailed = errors.New("generator failed")
)

type failingBuildSBOMGenerator struct{ err error }

type failingReadDirFS struct {
	fs.FS
	err error
}

func (f failingReadDirFS) ReadDir(string) ([]fs.DirEntry, error) {
	return nil, f.err
}

func (f failingBuildSBOMGenerator) GenerateBuildSBOM(context.Context, projecttype.Type, string, string, io.Writer) error {
	return f.err
}

func (g *fakeGit) Run(_ context.Context, args ...string) (string, error) {
	if len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--short" {
		if g.sha == "" {
			return "", errors.New("not a git repo") //nolint:err113 // test mock error
		}

		return g.sha, nil
	}

	return "", nil
}

func TestGenerate_NPM_BuildLayer_HappyPath(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`{"name":"demo","version":"1.2.3"}`))
	// The aggregate Build SBOM the bash expects at <root>/bom.json.
	fsys.WriteFile("bom.json", []byte(`{"bomFormat":"CycloneDX"}`))
	fsys.Chdir()

	var out bytes.Buffer

	err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{sha: "abc1234"}, nil, &out, io.Discard, appsbom.GenerateInput{ //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Layers: "build", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})
	if err != nil {
		t.Fatalf("Generate: %v\nstdout: %s", err, out.String())
	}

	wantFile := "demo-1.2.3-abc1234-build-sbom.cyclonedx.json"
	if _, err := os.Stat(fsys.Path(wantFile)); err != nil {
		t.Errorf("expected %s, got: %v", wantFile, err)
	}

	if !strings.Contains(out.String(), "✓ "+wantFile) {
		t.Errorf("missing success line:\n%s", out.String())
	}
}

func TestGenerate_NPM_AnalyzedArtifact_TGZ(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`{"name":"demo","version":"1.2.3"}`))
	// The bash searches ./release-artifacts then ".". Use cwd path.
	fsys.WriteFile("demo-1.2.3.tgz", []byte("fake-tarball"))
	fsys.Chdir()

	syft := &fakeSyft{}

	err := appsbom.Generate(context.Background(), syft, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		Layers: "analyzed-artifact", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// One syft invocation that emits both SPDX and CycloneDX (multi-output).
	if len(syft.calls) != 1 {
		t.Errorf("expected 1 syft call, got %d: %+v", len(syft.calls), syft.calls)
	}

	if got := len(syft.calls[0].outputs); got != 2 {
		t.Errorf("expected 2 output formats in the single call, got %d: %+v", got, syft.calls[0].outputs)
	}
	// No SHA → SHA-less analyzed basename.
	want := "demo-1.2.3-analyzed-tararchive-sbom.cyclonedx.json"
	if _, err := os.Stat(fsys.Path(want)); err != nil {
		t.Errorf("expected %s, got: %v", want, err)
	}

	if _, err := os.Stat(fsys.Path("demo-1.2.3-sboms.zip")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected no default zip archive, got err=%v", err)
	}
}

func TestGenerate_GoArtifactLayer_ScansExecutableFromReleaseArtifacts(t *testing.T) {
	fsys := testfs.NewReal(t)

	bin := fsys.WriteFile(filepath.Join("release-artifacts", "demo"), []byte("binary"))
	if err := os.Chmod(bin, 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	fsys.Chdir()

	syft := &fakeSyft{}
	if err := appsbom.Generate(context.Background(), syft, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		ProjectType: "go",
		Name:        "demo",  //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Version:     "1.2.3", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Layers:      "analyzed-artifact",
	}); err != nil {
		t.Fatal(err)
	}

	if len(syft.calls) != 1 || syft.calls[0].target != filepath.Join("release-artifacts", "demo") {
		t.Fatalf("syft calls = %+v", syft.calls)
	}

	for _, want := range []string{
		"demo-analyzed-binary-sbom.spdx.json",
		"demo-analyzed-binary-sbom.cyclonedx.json",
	} {
		if _, err := os.Stat(fsys.Path(want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}
}

func TestGenerate_GoArtifactLayer_ScansDownloadedArtifactWithoutExecutableBit(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join("release-artifacts", "linux-amd64", "demo-linux-amd64"), []byte("binary"))
	fsys.Chdir()

	syft := &fakeSyft{}
	if err := appsbom.Generate(context.Background(), syft, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		ProjectType: "go",
		Name:        "demo",
		Version:     "1.2.3",
		Layers:      "analyzed-artifact",
	}); err != nil {
		t.Fatal(err)
	}

	if len(syft.calls) != 1 || syft.calls[0].target != filepath.Join("release-artifacts", "linux-amd64", "demo-linux-amd64") {
		t.Fatalf("syft calls = %+v", syft.calls)
	}
}

func TestGenerate_GoArtifactLayer_ScansExtractedBinaryWithoutExecutableBit(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join("release-artifacts", "binaries", "demo-binaries-amd64", "demo-linux-amd64"), []byte("binary"))
	fsys.Chdir()

	syft := &fakeSyft{}
	if err := appsbom.Generate(context.Background(), syft, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		ProjectType: "go",
		Name:        "demo",
		Version:     "1.2.3",
		Layers:      "analyzed-artifact",
	}); err != nil {
		t.Fatal(err)
	}

	if len(syft.calls) != 1 || syft.calls[0].target != filepath.Join("release-artifacts", "binaries", "demo-binaries-amd64", "demo-linux-amd64") {
		t.Fatalf("syft calls = %+v", syft.calls)
	}
}

func TestGenerate_GoBuildLayer_PrefersNamedBOM(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join("release-artifacts", "aaa", "bom.json"), []byte(`{"bomFormat":"CycloneDX","name":"wrong"}`))
	fsys.WriteFile(filepath.Join("release-artifacts", "demo", "bom.json"), []byte(`{"bomFormat":"CycloneDX","name":"demo"}`))
	fsys.Chdir()

	if err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		ProjectType: "go",
		Name:        "demo",
		Version:     "1.2.3",
		Layers:      "build",
	}); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(fsys.Path("demo-1.2.3-build-sbom.cyclonedx.json"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(body), `"name":"demo"`) {
		t.Fatalf("selected wrong Go BOM: %s", string(body))
	}
}

func TestGenerate_CargoArtifactLayer_FallsBackToTargetReleaseAndSkipsDebugInfo(t *testing.T) {
	fsys := testfs.NewReal(t)
	bin := fsys.WriteFile(filepath.Join("target", "release", "demo"), []byte("binary"))

	debugInfo := fsys.WriteFile(filepath.Join("target", "release", "demo.d"), []byte("debug info"))
	for _, path := range []string{bin, debugInfo} {
		if err := os.Chmod(path, 0o755); err != nil { //nolint:gosec // test fixture
			t.Fatal(err)
		}
	}

	fsys.Chdir()

	syft := &fakeSyft{}
	if err := appsbom.Generate(context.Background(), syft, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		ProjectType: "cargo",
		Name:        "demo",
		Version:     "1.2.3",
		Layers:      "analyzed-artifact",
	}); err != nil {
		t.Fatal(err)
	}

	if len(syft.calls) != 1 || syft.calls[0].target != filepath.Join("target", "release", "demo") {
		t.Fatalf("syft calls = %+v", syft.calls)
	}
}

func TestGenerate_GradleArtifactLayer_FiltersClassifierJars(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join("build", "libs", "demo-1.2.3.jar"), []byte("jar"))
	fsys.WriteFile(filepath.Join("build", "libs", "demo-1.2.3-sources.jar"), []byte("sources"))
	fsys.WriteFile(filepath.Join("build", "libs", "other-1.2.3.jar"), []byte("other"))
	fsys.Chdir()

	syft := &fakeSyft{}
	if err := appsbom.Generate(context.Background(), syft, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		ProjectType: "gradle",
		Name:        "demo",
		Version:     "1.2.3",
		Layers:      "analyzed-artifact",
	}); err != nil {
		t.Fatal(err)
	}

	if len(syft.calls) != 1 || syft.calls[0].target != filepath.Join("build", "libs", "demo-1.2.3.jar") {
		t.Fatalf("syft calls = %+v", syft.calls)
	}
}

func TestGenerate_PythonArtifactLayer_TrimsWheelAndSdistNames(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join("dist", "demo-1.2.3-py3-none-any.whl"), []byte("wheel"))
	fsys.WriteFile(filepath.Join("dist", "demo-1.2.3.tar.gz"), []byte("sdist"))
	fsys.Chdir()

	syft := &fakeSyft{}
	if err := appsbom.Generate(context.Background(), syft, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		ProjectType: "python",
		Name:        "demo",
		Version:     "1.2.3",
		Layers:      "analyzed-artifact",
	}); err != nil {
		t.Fatal(err)
	}

	// Every other test in this file names what syft was pointed at; this one
	// checked only how many times it was called, so scanning the same wheel
	// twice would have passed.
	wantTargets := []string{
		filepath.Join("dist", "demo-1.2.3-py3-none-any.whl"),
		filepath.Join("dist", "demo-1.2.3.tar.gz"),
	}

	gotTargets := make([]string, 0, len(syft.calls))
	for _, call := range syft.calls {
		gotTargets = append(gotTargets, call.target)
	}

	if !reflect.DeepEqual(gotTargets, wantTargets) {
		t.Errorf("syft targets = %v, want %v", gotTargets, wantTargets)
	}

	for _, want := range []string{
		"demo-1.2.3-py3-none-any-analyzed-wheel-sbom.cyclonedx.json",
		"demo-1.2.3-analyzed-wheel-sbom.cyclonedx.json",
	} {
		if _, err := os.Stat(fsys.Path(want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}
}

func TestGenerate_NPM_BuildLayer_MissingBOMWarns(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`{"name":"demo","version":"1.2.3"}`))
	fsys.Chdir()

	var out bytes.Buffer

	err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, nil, &out, io.Discard, appsbom.GenerateInput{
		Layers: "build",
	})
	if err == nil {
		t.Fatal("expected error when no SBOMs generated")
	}

	if !strings.Contains(out.String(), "No npm Build SBOM found") {
		t.Errorf("missing warn line:\n%s", out.String())
	}
}

func TestGenerate_Maven_BuildAndArtifactLayers(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("pom.xml", []byte("<project/>"))
	fsys.WriteFile(filepath.Join("target", "bom.json"), []byte(`{"bomFormat":"CycloneDX"}`))
	fsys.WriteFile(filepath.Join("target", "demo-1.0.0.jar"), []byte("fake-jar"))
	fsys.Chdir()

	mvn := &fakeMaven{answers: map[string]string{
		"project.version":    "1.0.0", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"project.artifactId": "demo",  //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}}

	err := appsbom.Generate(context.Background(), &fakeSyft{}, mvn, &fakeGit{sha: "abc1234"}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		Layers: "build,analyzed-artifact", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	for _, want := range []string{
		"demo-1.0.0-abc1234-build-sbom.cyclonedx.json",
		"demo-1.0.0-abc1234-analyzed-jar-sbom.spdx.json",
		"demo-1.0.0-abc1234-analyzed-jar-sbom.cyclonedx.json",
	} {
		if _, err := os.Stat(fsys.Path(want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}
}

func TestGenerate_ExplicitProjectTypeSkipsAutoDetect(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("pom.xml", []byte("<project/>"))
	fsys.WriteFile("package.json", []byte(`{"name":"demo","version":"1.2.3"}`))
	fsys.WriteFile("bom.json", []byte(`{"bomFormat":"CycloneDX"}`))
	fsys.Chdir()

	var out bytes.Buffer
	if err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, nil, &out, io.Discard, appsbom.GenerateInput{
		ProjectType: "npm",
		Layers:      "build",
	}); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(out.String(), "Auto-detected project type") {
		t.Errorf("expected explicit project type to skip auto-detect:\n%s", out.String())
	}

	if !strings.Contains(out.String(), "Project type: npm") {
		t.Errorf("missing explicit project type line:\n%s", out.String())
	}
}

func TestGenerate_InvalidExplicitProjectTypeErrors(t *testing.T) {
	err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		ProjectType: "unknown",
	})
	// A bad --project-type is a bad flag: ErrUsage, exit 2.
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	if !strings.Contains(err.Error(), `invalid --project-type "unknown"`) {
		t.Fatalf("err = %v, want it to quote the rejected value", err)
	}

	if !strings.Contains(err.Error(), "valid:") {
		t.Errorf("expected valid-type list, got: %v", err)
	}
}

func TestGenerate_MissingRequestedBuildLayerFailsClosed(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("pom.xml", []byte("<project/>"))
	fsys.WriteFile(filepath.Join("release-artifacts", "demo-1.0.0.jar"), []byte("fake-jar"))
	fsys.Chdir()

	var out bytes.Buffer

	err := appsbom.Generate(context.Background(), &fakeSyft{}, &fakeMaven{answers: map[string]string{
		"project.version":    "1.0.0",
		"project.artifactId": "demo",
	}}, &fakeGit{}, nil, &out, io.Discard, appsbom.GenerateInput{
		Layers: "build,analyzed-artifact",
	})
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Fatalf("Generate err = %v, want ErrMissingInput\nstdout: %s", err, out.String())
	}

	if !strings.Contains(out.String(), "No Maven Build SBOM found") {
		t.Errorf("missing build warning:\n%s", out.String())
	}

	if _, statErr := os.Stat(fsys.Path("demo-1.0.0-analyzed-jar-sbom.cyclonedx.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("later layer unexpectedly ran: %v", statErr)
	}
}

func TestGenerate_GradleAndroidBuildLayerHarvestsRequestedBOM(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join("release-artifacts", "app", "build", "reports", "cyclonedx", "bom.json"), []byte(`{"bomFormat":"CycloneDX"}`))
	fsys.Chdir()

	if err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{sha: "abc1234"}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		ProjectType: "gradle-android",
		Layers:      "build",
		Name:        "android-app",
		Version:     "1.2.3",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(fsys.Path("android-app-1.2.3-abc1234-build-sbom.cyclonedx.json")); err != nil {
		t.Fatalf("Android Build SBOM was not assembled: %v", err)
	}
}

func TestGenerate_MavenArtifactLayer_AcceptsFinalNameJar(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("pom.xml", []byte("<project/>"))
	fsys.WriteFile(filepath.Join("target", "demo.jar"), []byte("fake-jar"))
	fsys.Chdir()

	if err := appsbom.Generate(context.Background(), &fakeSyft{}, &fakeMaven{answers: map[string]string{
		"project.version":    "1.0.0",
		"project.artifactId": "demo",
	}}, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{Layers: "analyzed-artifact"}); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"demo-analyzed-jar-sbom.spdx.json",
		"demo-analyzed-jar-sbom.cyclonedx.json",
	} {
		if _, err := os.Stat(fsys.Path(want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}
}

func TestGenerate_NPMScopedNameStripped(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`{"name":"@digg/example","version":"0.1.0"}`))
	fsys.WriteFile("bom.json", []byte(`{"bomFormat":"CycloneDX"}`))
	fsys.Chdir()

	var out bytes.Buffer
	if err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, nil, &out, io.Discard, appsbom.GenerateInput{
		Layers: "build",
	}); err != nil {
		t.Fatal(err)
	}
	// Scoped names are stripped — file should start with "example-".
	body := out.String()
	if !strings.Contains(body, "example-0.1.0-build-sbom.cyclonedx.json") {
		t.Errorf("expected scope-stripped filename, got:\n%s", body)
	}
}

func TestGenerate_UnknownLayerErrors(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`{"name":"demo","version":"1"}`))
	fsys.WriteFile("bom.json", []byte("{\"bomFormat\":\"CycloneDX\"}"))
	fsys.Chdir()

	err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		Layers: "build,bogus",
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	// The operator has to be able to fix the typo from the message alone.
	for _, want := range []string{"bogus", "build, analyzed-artifact, analyzed-container"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to mention %q", err, want)
		}
	}
}

func TestGenerate_CreateZipBundlesSBOMs(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`{"name":"demo","version":"1.2.3"}`))
	fsys.WriteFile("bom.json", []byte(`{"bomFormat":"CycloneDX"}`))
	fsys.Chdir()

	err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		Layers:    "build",
		CreateZip: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// The archive is what gets attached to a release, so its contents are the
	// contract: the SBOMs that were generated, byte-for-byte, and nothing else.
	// Asserting only that a file exists would pass on an empty archive.
	assertZipMembers(t, fsys.Path("demo-1.2.3-sboms.zip"), map[string]string{
		"demo-1.2.3-build-sbom.cyclonedx.json": `{"bomFormat":"CycloneDX"}`,
	})
}

// assertZipMembers pins the exact member set and bytes of an archive: a missing
// or unrelated entry, a duplicate name and an emptied member all fail here.
func assertZipMembers(t *testing.T, path string, want map[string]string) {
	t.Helper()

	reader, err := zip.OpenReader(path)
	require.NoError(t, err)

	defer func() { require.NoError(t, reader.Close()) }()

	got := map[string]string{}
	for _, member := range reader.File {
		require.NotContains(t, got, member.Name, "duplicate archive member")
		body, openErr := member.Open()
		require.NoError(t, openErr)

		content, readErr := io.ReadAll(body)
		require.NoError(t, readErr)
		require.NoError(t, body.Close())
		require.NotEmpty(t, content, "archive member %s is empty", member.Name)
		got[member.Name] = string(content)
	}

	require.Equal(t, want, got)
}

func TestGenerate_CreateZipFailureIsReturned(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`{"name":"demo","version":"1.2.3"}`))
	fsys.WriteFile("bom.json", []byte(`{"bomFormat":"CycloneDX"}`))
	fsys.MkdirAll("demo-1.2.3-sboms.zip")
	fsys.Chdir()

	err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		Layers:    "build",
		CreateZip: true,
	})
	if err == nil || !strings.Contains(err.Error(), "create SBOM ZIP") {
		t.Fatalf("zip failure = %v, want propagated error", err)
	}
}

func TestGenerate_RejectsSymlinkedWorkingDirectoryRoot(t *testing.T) {
	t.Parallel()

	base := t.TempDir()

	realDir := filepath.Join(base, "real")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}

	linked := filepath.Join(base, "linked")
	if err := os.Symlink(realDir, linked); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		WorkingDir:  linked,
		ProjectType: "go",
		Name:        "demo",
		Version:     "1.0.0",
		Layers:      "build",
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("symlinked workspace error = %v, want ErrValidation", err)
	}
}

func TestGenerate_PropagatesWorkspaceWalkErrors(t *testing.T) {
	t.Parallel()

	want := errWalkFailed
	input := failingReadDirFS{
		FS:  fstest.MapFS{"go.mod": &fstest.MapFile{Data: []byte("module example.com/demo\n")}},
		err: want,
	}

	err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		WorkingDir:  t.TempDir(),
		FS:          input,
		ProjectType: "go",
		Name:        "demo",
		Version:     "1.0.0",
		Layers:      "build",
	})
	if !errors.Is(err, want) {
		t.Fatalf("walk error = %v, want wrapped %v", err, want)
	}
}

func TestGenerate_RejectsSymlinkedSBOMCollectionInput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"demo","version":"1.0.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "bom.json"), []byte(`{"bomFormat":"CycloneDX"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(outside, filepath.Join(dir, "forged-sbom.layer.json")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		WorkingDir:  dir,
		ProjectType: "npm",
		Layers:      "build",
		CreateZip:   true,
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("symlinked SBOM input error = %v, want ErrValidation", err)
	}

	if body, readErr := os.ReadFile(outside); readErr != nil || string(body) != "outside" {
		t.Fatalf("outside symlink target changed: body=%q err=%v", body, readErr)
	}
}

func TestGenerate_BuildSBOMGeneratorFailureIsReturned(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("go.mod", []byte("module example.com/demo\n"))
	fsys.Chdir()

	want := errBuildSBOMGeneratorFailed

	err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, failingBuildSBOMGenerator{err: want}, io.Discard, io.Discard, appsbom.GenerateInput{
		Layers: "build",
	})
	if !errors.Is(err, want) {
		t.Fatalf("generator failure = %v, want wrapped %v", err, want)
	}
}

func TestGenerate_AutoDetectsProjectType(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("Cargo.toml", []byte("[package]\nname = \"demo\"\nversion = \"0.1.0\"\n"))
	fsys.WriteFile("bom.json", []byte("{\"bomFormat\":\"CycloneDX\"}"))
	fsys.Chdir()

	var out bytes.Buffer
	if err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, nil, &out, io.Discard, appsbom.GenerateInput{
		Layers: "build",
		// ProjectType empty → auto
	}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "Auto-detected project type: cargo") {
		t.Errorf("missing auto-detect line:\n%s", out.String())
	}
}

func TestGenerate_ResolvesCargoWorkspacePackageVersion(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("Cargo.toml", []byte("[workspace]\nmembers = ['member']\n\n[workspace.package]\nversion = '2.3.4'\n"))
	fsys.WriteFile("member/Cargo.toml", []byte("[package]\nname = 'demo'\nversion = { workspace = true }\n"))
	fsys.WriteFile("member/bom.json", []byte("{\"bomFormat\":\"CycloneDX\"}"))
	fsys.Chdir()

	if err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		WorkingDir: "member",
		Layers:     "build",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(fsys.Path("member/demo-2.3.4-build-sbom.cyclonedx.json")); err != nil {
		t.Errorf("expected workspace-versioned SBOM: %v", err)
	}
}

func TestGenerate_CargoWorkspaceVersionWithoutRootErrors(t *testing.T) {
	output := testfs.NewReal(t)
	input := testfs.NewMemory(t)
	input.WriteFile("Cargo.toml", []byte("[package]\nname = 'demo'\nversion = { workspace = true }\n"))

	err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		WorkingDir: output.Root,
		FS:         input.FS(),
		Layers:     "build",
	})
	// The domain has a sentinel for this; matching prose alone would keep
	// passing if the message were reworded and the sentinel dropped.
	if !errors.Is(err, domainsbom.ErrCargoWorkspaceVersionUnavailable) {
		t.Fatalf("workspace inheritance error = %v, want ErrCargoWorkspaceVersionUnavailable", err)
	}

	if !strings.Contains(err.Error(), "workspace root Cargo.toml") {
		t.Errorf("err = %v, want it to point at the workspace root", err)
	}
}

func TestGenerate_WorkingDirSubproject(t *testing.T) {
	fsys := testfs.NewReal(t)
	subdir := fsys.Path("subproject")
	fsys.WriteFile(filepath.Join("subproject", "package.json"), []byte(`{"name":"demo","version":"1.2.3"}`))
	fsys.WriteFile(filepath.Join("subproject", "bom.json"), []byte(`{"bomFormat":"CycloneDX"}`))
	fsys.Chdir()

	if err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		WorkingDir: "subproject",
		Layers:     "build",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(subdir, "demo-1.2.3-build-sbom.cyclonedx.json")); err != nil {
		t.Errorf("expected SBOM in subproject: %v", err)
	}
}

func TestGenerate_ReadsProjectMetadataFromFSWithoutChangingCwd(t *testing.T) {
	output := testfs.NewReal(t)
	input := testfs.NewMemory(t)
	input.WriteFile("package.json", []byte(`{"name":"mem-demo","version":"2.0.0"}`))
	input.WriteFile("bom.json", []byte(`{"bomFormat":"CycloneDX"}`))

	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	if genErr := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		WorkingDir: output.Root,
		FS:         input.FS(),
		Layers:     "build",
	}); genErr != nil {
		t.Fatal(genErr)
	}

	after, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	if after != before {
		t.Fatalf("cwd changed from %q to %q", before, after)
	}

	if _, err := os.Stat(output.Path("mem-demo-2.0.0-build-sbom.cyclonedx.json")); err != nil {
		t.Errorf("expected SBOM in output dir: %v", err)
	}
}

func TestGenerate_InvalidWorkingDirErrors(t *testing.T) {
	fsys := testfs.NewReal(t)

	err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		WorkingDir: fsys.Path("missing"),
	})
	// The cause has to survive the wrap: "no such file" is what tells the
	// operator the path is wrong rather than, say, unreadable.
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}

	if !strings.Contains(err.Error(), "working directory") {
		t.Errorf("err = %v, want it to name the working directory", err)
	}
}

func TestGenerate_ContainerLayer_NoImageFailsSummary(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	var out bytes.Buffer

	err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, nil, &out, io.Discard, appsbom.GenerateInput{
		ProjectType: "maven",
		Layers:      "analyzed-container",
		Version:     "1.0.0",
		Name:        "myapp",
	})
	if err == nil {
		t.Fatal("expected error when container image is missing")
	}

	if !strings.Contains(out.String(), "No container image specified") {
		t.Errorf("missing container warning:\n%s", out.String())
	}

	if !errors.Is(err, errs.ErrMissingInput) {
		t.Errorf("err = %v, want ErrMissingInput", err)
	}
}

func TestGenerate_SanitisesBranchNameInFilenames(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`{"name":"demo","version":"feat/awesome"}`))
	fsys.WriteFile("bom.json", []byte("{\"bomFormat\":\"CycloneDX\"}"))
	fsys.Chdir()

	var out bytes.Buffer
	if err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, nil, &out, io.Discard, appsbom.GenerateInput{
		Layers: "build",
	}); err != nil {
		t.Fatal(err)
	}
	// The `/` in feat/awesome must become `-` per sanitize_path_token.
	if !strings.Contains(out.String(), "demo-feat-awesome-build-sbom.cyclonedx.json") {
		t.Errorf("missing sanitised filename:\n%s", out.String())
	}
}

func TestGenerate_ContainerLayer_SanitisesSlashedVersion(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	if err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		ProjectType:    "maven",
		Layers:         "analyzed-container",
		Version:        "feat/x",
		Name:           "myapp",
		ContainerImage: "ghcr.io/org/myapp:feat-x",
	}); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"myapp-feat-x-analyzed-container-sbom.spdx.json",
		"myapp-feat-x-analyzed-container-sbom.cyclonedx.json",
	} {
		if _, err := os.Stat(fsys.Path(want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}

	if _, err := os.Stat(fsys.Path("myapp-feat")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("unexpected subdirectory from unsanitised version, err=%v", err)
	}
}

// TestGenerate_CargoArtifactLayer_IgnoresBuildScriptsUnderTargetRelease pins
// that only the binaries directly under target/release are release artifacts:
// cargo's build/<crate>/build-script-build executables live one level down
// and were scanned as if they were the release binary.
func TestGenerate_CargoArtifactLayer_IgnoresBuildScriptsUnderTargetRelease(t *testing.T) {
	fsys := testfs.NewReal(t)
	bin := fsys.WriteFile(filepath.Join("target", "release", "demo"), []byte("binary"))
	script := fsys.WriteFile(filepath.Join("target", "release", "build", "demo-abc", "build-script-build"), []byte("script"))

	for _, path := range []string{bin, script} {
		if err := os.Chmod(path, 0o755); err != nil { //nolint:gosec // test fixture
			t.Fatal(err)
		}
	}

	fsys.Chdir()

	syft := &fakeSyft{}
	if err := appsbom.Generate(context.Background(), syft, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		ProjectType: "cargo",
		Name:        "demo",
		Version:     "1.2.3",
		Layers:      "analyzed-artifact",
	}); err != nil {
		t.Fatal(err)
	}

	if len(syft.calls) != 1 || syft.calls[0].target != filepath.Join("target", "release", "demo") {
		t.Fatalf("syft calls = %+v, want only the release binary", syft.calls)
	}
}
