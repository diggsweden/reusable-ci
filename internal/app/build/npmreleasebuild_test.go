// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

var errMissingTestScript = errors.New("missing script: test")

type fakeNPMRunner struct {
	calls  [][]string
	failOn map[string]error // first-arg → error
}

func (f *fakeNPMRunner) RunInherit(_ context.Context, _ string, _, _ io.Writer, args ...string) error {
	f.calls = append(f.calls, args)

	if len(args) > 0 && f.failOn != nil {
		if err := f.failOn[args[0]]; err != nil {
			return err
		}
	}

	return nil
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
