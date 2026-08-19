// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

type recordingSummarySink struct{ buf bytes.Buffer }

func (s *recordingSummarySink) Append(_ context.Context, markdown string) error {
	_, _ = s.buf.WriteString(markdown)

	return nil
}

// goRunArgs returns the joined args of every recorded go invocation, so tests
// can assert which steps ran and in what order.
func goRunArgs(calls []appbuild.GoRunInput) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, strings.Join(c.Args, " "))
	}

	return out
}

// assertGoSequence asserts the exact list of go steps, by their leading
// verb. The full argv of each step is pinned by the unit tests in
// go_test.go; what matters here is which steps ran, and in what order.
func assertGoSequence(t *testing.T, calls []appbuild.GoRunInput, want ...string) {
	t.Helper()

	got := goRunArgs(calls)
	if len(got) != len(want) {
		t.Fatalf("go tool calls = %v, want %v", got, want)
	}

	for i, prefix := range want {
		if !strings.HasPrefix(got[i], prefix) {
			t.Errorf("call %d = %q, want it to start with %q", i, got[i], prefix)
		}
	}
}

func newGoModDir(t *testing.T) string {
	t.Helper()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("go.mod", []byte("module github.com/org/app\n"))

	return fsys.Root
}

func TestGoReleaseBuild_RunsFullSequenceInOrder(t *testing.T) {
	t.Parallel()
	dir := newGoModDir(t)
	goTool := &fakeGoTool{}
	sbomTool := &fakeCycloneDXGoModTool{}
	summary := &recordingSummarySink{}

	var out bytes.Buffer

	err := appbuild.GoReleaseBuild(context.Background(), summary, goTool, sbomTool, &out, &out, appbuild.GoReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir, EnableBuildSBOM: true},
		Version:             "1.2.3",
		Platforms:           "linux/amd64",
	})
	if err != nil {
		t.Fatal(err)
	}

	assertGoSequence(t, goTool.calls, "mod download", "test", "build")

	// SBOM tool ran once.
	if len(sbomTool.calls) != 1 {
		t.Errorf("sbom calls = %d, want 1", len(sbomTool.calls))
	}

	// Summary mentions the resolved metadata.
	if s := summary.buf.String(); !strings.Contains(s, "app") || !strings.Contains(s, "1.2.3") {
		t.Errorf("summary = %q", s)
	}
}

func TestGoReleaseBuild_SkipTests(t *testing.T) {
	t.Parallel()
	dir := newGoModDir(t)
	goTool := &fakeGoTool{}

	var out bytes.Buffer

	err := appbuild.GoReleaseBuild(context.Background(), &recordingSummarySink{}, goTool, &fakeCycloneDXGoModTool{}, &out, &out, appbuild.GoReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir, SkipTests: true, EnableBuildSBOM: true},
		Version:             "1.2.3",
		Platforms:           "linux/amd64",
	})
	if err != nil {
		t.Fatal(err)
	}

	// The whole remaining sequence, not just the absence of "test": a run
	// that skipped everything would satisfy "no test step ran" too.
	assertGoSequence(t, goTool.calls, "mod download", "build")
}

func TestGoReleaseBuild_NoBuildSBOM(t *testing.T) {
	t.Parallel()
	dir := newGoModDir(t)
	sbomTool := &fakeCycloneDXGoModTool{}
	goTool := &fakeGoTool{}

	var out bytes.Buffer

	err := appbuild.GoReleaseBuild(context.Background(), &recordingSummarySink{}, goTool, sbomTool, &out, &out, appbuild.GoReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir, EnableBuildSBOM: false},
		Version:             "1.2.3",
		Platforms:           "linux/amd64",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(sbomTool.calls) != 0 {
		t.Errorf("sbom ran despite EnableBuildSBOM=false: %d calls", len(sbomTool.calls))
	}

	// Dropping the SBOM must drop only the SBOM.
	assertGoSequence(t, goTool.calls, "mod download", "test", "build")
}

func TestGoReleaseBuild_VersionFromRefName(t *testing.T) {
	t.Parallel()
	dir := newGoModDir(t)
	goTool := &fakeGoTool{}

	var out bytes.Buffer

	// No --version; the tag ref-name supplies it (v-prefix stripped).
	err := appbuild.GoReleaseBuild(context.Background(), &recordingSummarySink{}, goTool, &fakeCycloneDXGoModTool{}, &out, &out, appbuild.GoReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir},
		RefName:             "v4.5.6",
		Platforms:           "linux/amd64",
	})
	if err != nil {
		t.Fatal(err)
	}

	// The compile ldflags carry the resolved version.
	compile := goRunArgs(goTool.calls)[len(goTool.calls)-1]
	if !strings.Contains(compile, "main.version=4.5.6") {
		t.Errorf("compile ldflags missing version: %q", compile)
	}
}

func TestGoReleaseBuild_MissingGoModFails(t *testing.T) {
	t.Parallel()

	goTool := &fakeGoTool{}
	sbomTool := &fakeCycloneDXGoModTool{}

	err := appbuild.GoReleaseBuild(context.Background(), &recordingSummarySink{}, goTool, sbomTool, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.GoReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: t.TempDir()},
		Version:             "1.2.3",
		Platforms:           "linux/amd64",
	})
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Fatalf("err = %v, want ErrMissingInput", err)
	}

	// Metadata is resolved first precisely so an unusable module costs
	// nothing: no download, no test run, no compile.
	if len(goTool.calls) != 0 || len(sbomTool.calls) != 0 {
		t.Errorf("ran tools before resolving metadata: go=%v sbom=%d", goRunArgs(goTool.calls), len(sbomTool.calls))
	}
}
