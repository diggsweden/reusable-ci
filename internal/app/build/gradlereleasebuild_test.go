// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
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

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
)

// recordingGradle records every gradle invocation (fakeGradle keeps only the
// last), so the orchestrator's call sequence can be asserted.
type recordingGradle struct{ calls [][]string }

func (g *recordingGradle) RunInherit(_ context.Context, _, _ io.Writer, args ...string) error {
	g.calls = append(g.calls, args)

	return nil
}

func (g *recordingGradle) RunInDirInherit(_ context.Context, _ string, _, _ io.Writer, args ...string) error {
	g.calls = append(g.calls, args)

	return nil
}

func newGradleDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "gradlew"), []byte("#!/bin/sh\n"), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "gradle.properties"), []byte("version=1.0.0\n"), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	return dir
}

// assertGradleCalls asserts the exact sequence of gradle invocations and
// the exact argv of each.
func assertGradleCalls(t *testing.T, got [][]string, want ...[]string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("gradle invocations = %v, want %v", got, want)
	}

	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("invocation %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestGradleReleaseBuild_RunsTasksAndMakesWrapperExecutable(t *testing.T) {
	t.Parallel()
	dir := newGradleDir(t)
	ops := &recordingGradle{}

	var out bytes.Buffer

	err := appbuild.GradleReleaseBuild(context.Background(), &recordingSummarySink{}, ops, &out, &out, appbuild.GradleReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir, EnableBuildSBOM: false},
		Tasks:               "assemble check",
		JavaVersion:         "25",
	})
	if err != nil {
		t.Fatal(err)
	}

	// One invocation carrying both tasks as separate argv entries -- the
	// task string is split here rather than re-split by a shell. The
	// previous assertion joined every call into one string and searched it,
	// which could not tell one invocation from several, nor argv entries
	// from a single space-joined one.
	assertGradleCalls(t, ops.calls, []string{"assemble", "check"})

	info, err := os.Stat(filepath.Join(dir, "gradlew"))
	if err != nil {
		t.Fatalf("stat gradlew: %v", err)
	}

	if info.Mode().Perm()&0o111 != 0o111 {
		t.Errorf("gradlew mode = %v, want executable by all", info.Mode().Perm())
	}
}

func TestGradleReleaseBuild_SkipTestsAppendsFlag(t *testing.T) {
	t.Parallel()

	ops := &recordingGradle{}

	err := appbuild.GradleReleaseBuild(context.Background(), &recordingSummarySink{}, ops, io.Discard, io.Discard, appbuild.GradleReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newGradleDir(t), SkipTests: true, EnableBuildSBOM: false},
		Tasks:               "assemble",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Appended after the tasks, as two argv entries.
	assertGradleCalls(t, ops.calls, []string{"assemble", "-x", "test"})
}

func TestGradleReleaseBuild_MissingWrapperFails(t *testing.T) {
	t.Parallel()

	ops := &recordingGradle{}
	summary := &recordingSummarySink{}

	err := appbuild.GradleReleaseBuild(context.Background(), summary, ops, io.Discard, io.Discard, appbuild.GradleReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: t.TempDir()},
		Tasks:               "assemble",
	})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want a not-exist error", err)
	}

	// The wrapper is made executable first, so a checkout without one
	// costs nothing: no gradle run and no summary claiming a build.
	if len(ops.calls) != 0 {
		t.Errorf("ran gradle without a wrapper: %v", ops.calls)
	}

	if summary.buf.Len() != 0 {
		t.Errorf("wrote a summary for a build that never started: %q", summary.buf.String())
	}
}

// TestGradleReleaseBuild_BuildSBOMIsBestEffort pins the documented
// best-effort contract: a Build SBOM that cannot be generated warns and
// lets the release continue.
//
// It also pins what the summary says while that happens, which is not
// what it means -- see docs/open-questions.md.
func TestGradleReleaseBuild_BuildSBOMIsBestEffort(t *testing.T) {
	t.Parallel()

	ops := &recordingGradle{}
	summary := &recordingSummarySink{}

	var stderr bytes.Buffer

	// EnableBuildSBOM with no SBOMToolVersion: the version is required, so
	// generation fails before the tool is reached.
	err := appbuild.GradleReleaseBuild(context.Background(), summary, ops, io.Discard, &stderr, appbuild.GradleReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newGradleDir(t), EnableBuildSBOM: true},
		Tasks:               "assemble",
	})
	if err != nil {
		t.Fatalf("a failed Build SBOM must not fail the release: %v", err)
	}

	if !strings.Contains(stderr.String(), "SBOM generation failed (continuing)") {
		t.Errorf("no warning about the failed SBOM: %q", stderr.String())
	}

	// The build itself still ran.
	assertGradleCalls(t, ops.calls, []string{"assemble"})

	// Recorded, not endorsed: the summary reports "release blocked" on a
	// release that was not blocked, and says the same for a deliberately
	// disabled SBOM.
	if !strings.Contains(summary.buf.String(), "release blocked") {
		t.Errorf("summary = %q", summary.buf.String())
	}
}
