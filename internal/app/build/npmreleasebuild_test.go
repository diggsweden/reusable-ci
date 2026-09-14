// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"github.com/stretchr/testify/require"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

var errMissingTestScript = errors.New("missing script: test")

type fakeNPMRunner struct {
	calls   [][]string
	failOn  map[string]error // first-arg → error
	dirs    []string
	outputs []io.Writer
	errors  []io.Writer
}

func (f *fakeNPMRunner) RunInherit(_ context.Context, dir string, stdout, stderr io.Writer, args ...string) error {
	f.calls = append(f.calls, args)
	f.dirs = append(f.dirs, dir)
	f.outputs = append(f.outputs, stdout)
	f.errors = append(f.errors, stderr)

	if len(args) > 0 && f.failOn != nil {
		if err := f.failOn[args[0]]; err != nil {
			return err
		}
	}

	if len(args) == 4 && args[0] == "pack" && args[1] == "--json" && args[2] == "--pack-destination" {
		if err := os.WriteFile(filepath.Join(args[3], "org-app-1.0.0.tgz"), npmTarballBytes("1.0.0"), 0o600); err != nil {
			return err
		}

		_, err := io.WriteString(stdout, `[{"name":"@org/app","version":"1.0.0","filename":"org-app-1.0.0.tgz"}]`)

		return err
	}

	if len(args) == 6 && args[0] == "--yes" {
		return os.WriteFile(args[5], []byte(`{"bomFormat":"CycloneDX","specVersion":"1.6","version":1}`), 0o600)
	}

	return nil
}

func npmTarballBytes(version string) []byte {
	var buffer bytes.Buffer

	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	body := `{"name":"@org/app","version":"` + version + `"}`
	_ = tarWriter.WriteHeader(&tar.Header{Name: "package/package.json", Mode: 0o600, Size: int64(len(body))})
	_, _ = io.WriteString(tarWriter, body)
	_ = tarWriter.Close()
	_ = gzipWriter.Close()

	return buffer.Bytes()
}

func (f *fakeNPMRunner) firstArgs() []string {
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, c[0])
	}

	return out
}

func newNPMDir(t *testing.T) string {
	t.Helper()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`{"name":"@org/app","version":"1.0.0","scripts":{"build":"tsc"}}`))

	return fsys.Root
}

func TestNPMReleaseBuild_RunsFullSequence(t *testing.T) {
	t.Parallel()
	dir := newNPMDir(t)
	npmRunner := &fakeNPMRunner{}
	npxRunner := &fakeNPMRunner{}

	var out bytes.Buffer

	err := appbuild.NPMReleaseBuild(context.Background(), &recordingSummarySink{}, npmRunner, npxRunner, output.Annotator{}, &out, &out, appbuild.NPMReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir, EnableBuildSBOM: true},
		PackageScope:        "@org",
		SBOMToolVersion:     "4.2.1",
	})
	if err != nil {
		t.Fatal(err)
	}

	// npm ran: ci, test, run (build script), pack — in order.
	if got := strings.Join(npmRunner.firstArgs(), ","); got != "ci,test,run,pack" {
		t.Errorf("npm calls = %v, want ci,test,run,pack", npmRunner.firstArgs())
	}

	// npx ran cyclonedx-npm once with the pinned version.
	if len(npxRunner.calls) != 1 || !strings.Contains(strings.Join(npxRunner.calls[0], " "), "@cyclonedx/cyclonedx-npm@4.2.1") {
		t.Errorf("npx calls = %v", npxRunner.calls)
	}

	require.Equal(t, [][]string{{"ci"}, {"test"}, {"run", "build"}, {"pack", "--json", "--pack-destination", npmRunner.calls[3][3]}}, npmRunner.calls)
	require.Equal(t, []string{"--yes", "@cyclonedx/cyclonedx-npm@4.2.1", "--output-format", "json", "--output", npxRunner.calls[0][5]}, npxRunner.calls[0])
	require.Equal(t, "bom.json", filepath.Base(npxRunner.calls[0][5]))
	require.Equal(t, dir, filepath.Dir(filepath.Dir(npxRunner.calls[0][5])))

	for i, callDir := range npmRunner.dirs {
		require.Equal(t, dir, callDir)
		require.Same(t, &out, npmRunner.errors[i])

		if i < 3 {
			require.Same(t, &out, npmRunner.outputs[i])
		}
	}

	require.Equal(t, []string{dir}, npxRunner.dirs)
	require.Same(t, &out, npxRunner.outputs[0])
	require.Same(t, &out, npxRunner.errors[0])

	body, err := os.ReadFile(filepath.Join(dir, "org-app-1.0.0.tgz"))
	require.NoError(t, err)
	require.Equal(t, npmTarballBytes("1.0.0"), body)
	body, err = os.ReadFile(filepath.Join(dir, "bom.json"))
	require.NoError(t, err)
	require.True(t, json.Valid(body))
	require.JSONEq(t, `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1}`, string(body))
}

func TestNPMReleaseBuild_SkipTests(t *testing.T) {
	t.Parallel()

	npmRunner := &fakeNPMRunner{}

	err := appbuild.NPMReleaseBuild(context.Background(), &recordingSummarySink{}, npmRunner, &fakeNPMRunner{}, output.Annotator{}, io.Discard, io.Discard, appbuild.NPMReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newNPMDir(t), SkipTests: true, EnableBuildSBOM: false},
	})
	if err != nil {
		t.Fatal(err)
	}

	// The rest of the sequence, not just the absence of "test": scanning
	// for a missing step passes equally on a run that did nothing.
	if got := strings.Join(npmRunner.firstArgs(), ","); got != "ci,run,pack" {
		t.Errorf("npm calls = %v, want ci,run,pack", npmRunner.firstArgs())
	}
}

func TestNPMReleaseBuild_SoftTestDoesNotFailBuild(t *testing.T) {
	t.Parallel()

	npmRunner := &fakeNPMRunner{failOn: map[string]error{"test": errMissingTestScript}}

	var stderr bytes.Buffer

	// A failing `npm test` must NOT fail the release build; pack still runs.
	err := appbuild.NPMReleaseBuild(context.Background(), &recordingSummarySink{}, npmRunner, &fakeNPMRunner{}, output.NewAnnotator(&stderr, output.FormatGitHub), io.Discard, io.Discard, appbuild.NPMReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newNPMDir(t), EnableBuildSBOM: false},
	})
	if err != nil {
		t.Fatalf("soft test failure should not fail build: %v", err)
	}

	// The whole sequence continues unchanged -- test ran, failed, and the
	// build and pack steps still happened in order.
	if got := strings.Join(npmRunner.firstArgs(), ","); got != "ci,test,run,pack" {
		t.Errorf("npm calls = %v, want ci,test,run,pack", npmRunner.firstArgs())
	}

	// The failure is soft, not silent.
	if !strings.Contains(stderr.String(), "npm test failed or no tests configured (continuing)") {
		t.Errorf("no warning about the failed test run: %q", stderr.String())
	}
}

func TestNPMReleaseBuild_NoBuildSBOM(t *testing.T) {
	t.Parallel()

	npxRunner := &fakeNPMRunner{}
	npmRunner := &fakeNPMRunner{}

	err := appbuild.NPMReleaseBuild(context.Background(), &recordingSummarySink{}, npmRunner, npxRunner, output.Annotator{}, io.Discard, io.Discard, appbuild.NPMReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newNPMDir(t), EnableBuildSBOM: false},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(npxRunner.calls) != 0 {
		t.Errorf("npx ran despite EnableBuildSBOM=false: %v", npxRunner.calls)
	}

	// Dropping the SBOM must drop only the SBOM.
	if got := strings.Join(npmRunner.firstArgs(), ","); got != "ci,test,run,pack" {
		t.Errorf("npm calls = %v, want ci,test,run,pack", npmRunner.firstArgs())
	}
}

func TestNPMReleaseBuild_ScopeMismatchFails(t *testing.T) {
	t.Parallel()

	npmRunner := &fakeNPMRunner{}
	npxRunner := &fakeNPMRunner{}
	summary := &recordingSummarySink{}

	var stderr bytes.Buffer

	err := appbuild.NPMReleaseBuild(context.Background(), summary, npmRunner, npxRunner, output.NewAnnotator(&stderr, output.FormatGitHub), io.Discard, io.Discard, appbuild.NPMReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newNPMDir(t)},
		PackageScope:        "@other",
	})
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}

	// The scope decides where the package would be published, so it is
	// checked before anything is installed, built or packed.
	if len(npmRunner.calls) != 0 || len(npxRunner.calls) != 0 {
		t.Errorf("ran tools despite the scope mismatch: npm=%v npx=%v", npmRunner.calls, npxRunner.calls)
	}

	if summary.buf.Len() != 0 {
		t.Errorf("wrote a summary for a build that never started: %q", summary.buf.String())
	}

	if !strings.Contains(stderr.String(), "must be scoped as @other/<pkg>") {
		t.Errorf("stderr = %q", stderr.String())
	}
}
