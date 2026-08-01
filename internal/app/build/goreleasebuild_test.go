// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
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

	// go tool ran: download, test, compile (in that order).
	args := goRunArgs(goTool.calls)
	if len(args) != 3 || !strings.HasPrefix(args[0], "mod download") || !strings.HasPrefix(args[1], "test") || !strings.HasPrefix(args[2], "build") {
		t.Fatalf("go tool calls = %v, want [download test build]", args)
	}

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

	for _, a := range goRunArgs(goTool.calls) {
		if strings.HasPrefix(a, "test") {
			t.Errorf("test ran despite --skip-tests: %v", goRunArgs(goTool.calls))
		}
	}
}

func TestGoReleaseBuild_NoBuildSBOM(t *testing.T) {
	t.Parallel()
	dir := newGoModDir(t)
	sbomTool := &fakeCycloneDXGoModTool{}

	var out bytes.Buffer

	err := appbuild.GoReleaseBuild(context.Background(), &recordingSummarySink{}, &fakeGoTool{}, sbomTool, &out, &out, appbuild.GoReleaseBuildInput{
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

	err := appbuild.GoReleaseBuild(context.Background(), &recordingSummarySink{}, &fakeGoTool{}, &fakeCycloneDXGoModTool{}, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.GoReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: t.TempDir()},
		Version:             "1.2.3",
		Platforms:           "linux/amd64",
	})
	if err == nil || !strings.Contains(err.Error(), "go.mod") {
		t.Fatalf("err = %v, want go.mod error", err)
	}
}
