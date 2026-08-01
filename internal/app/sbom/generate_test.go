// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appsbom "github.com/diggsweden/reusable-ci/v3/internal/app/sbom"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
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
	fsys.WriteFile(filepath.Join("release-artifacts", "aaa", "bom.json"), []byte(`{"name":"wrong"}`))
	fsys.WriteFile(filepath.Join("release-artifacts", "demo", "bom.json"), []byte(`{"name":"demo"}`))
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

	if len(syft.calls) != 2 {
		t.Fatalf("syft calls = %+v", syft.calls)
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
	fsys.WriteFile("bom.json", []byte(`{}`))
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
	if err == nil || !strings.Contains(err.Error(), `invalid --project-type "unknown"`) {
		t.Fatalf("err = %v", err)
	}

	if !strings.Contains(err.Error(), "valid:") {
		t.Errorf("expected valid-type list, got: %v", err)
	}
}

func TestGenerate_MissingBuildLayerDoesNotBlockArtifactLayer(t *testing.T) {
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
	if err != nil {
		t.Fatalf("Generate: %v\nstdout: %s", err, out.String())
	}

	if !strings.Contains(out.String(), "No Maven Build SBOM found") {
		t.Errorf("missing build warning:\n%s", out.String())
	}

	for _, want := range []string{
		"demo-1.0.0-analyzed-jar-sbom.spdx.json",
		"demo-1.0.0-analyzed-jar-sbom.cyclonedx.json",
	} {
		if _, err := os.Stat(fsys.Path(want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
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
	fsys.WriteFile("bom.json", []byte(`{}`))
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
	fsys.WriteFile("bom.json", []byte("{}"))
	fsys.Chdir()

	err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		Layers: "build,bogus",
	})
	if err == nil || !strings.Contains(err.Error(), "unknown layer") {
		t.Errorf("expected unknown-layer error, got: %v", err)
	}
}

func TestGenerate_CreateZipBundlesSBOMs(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`{"name":"demo","version":"1.2.3"}`))
	fsys.WriteFile("bom.json", []byte(`{}`))
	fsys.Chdir()

	err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{}, nil, io.Discard, io.Discard, appsbom.GenerateInput{
		Layers:    "build",
		CreateZip: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(fsys.Path("demo-1.2.3-sboms.zip")); err != nil {
		t.Errorf("expected zip: %v", err)
	}
}

func TestGenerate_AutoDetectsProjectType(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("Cargo.toml", []byte("[package]\nname = \"demo\"\nversion = \"0.1.0\"\n"))
	fsys.WriteFile("bom.json", []byte("{}"))
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

func TestGenerate_WorkingDirSubproject(t *testing.T) {
	fsys := testfs.NewReal(t)
	subdir := fsys.Path("subproject")
	fsys.WriteFile(filepath.Join("subproject", "package.json"), []byte(`{"name":"demo","version":"1.2.3"}`))
	fsys.WriteFile(filepath.Join("subproject", "bom.json"), []byte(`{}`))
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
	if err == nil || !strings.Contains(err.Error(), "working directory") {
		t.Errorf("err = %v, want working directory error", err)
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

	if err == nil || !strings.Contains(err.Error(), "no SBOM layers produced") {
		t.Errorf("missing summary error in err: %v", err)
	}
}

func TestGenerate_SanitisesBranchNameInFilenames(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`{"name":"demo","version":"feat/awesome"}`))
	fsys.WriteFile("bom.json", []byte("{}"))
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
