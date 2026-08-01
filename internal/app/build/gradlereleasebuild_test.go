// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
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

func gradleAllArgs(calls [][]string) string {
	parts := make([]string, 0, len(calls))
	for _, c := range calls {
		parts = append(parts, strings.Join(c, " "))
	}

	return strings.Join(parts, "\n")
}

func TestGradleReleaseBuild_RunsTasksAndMakesWrapperExecutable(t *testing.T) {
	t.Parallel()
	dir := newGradleDir(t)
	ops := &recordingGradle{}

	var out bytes.Buffer

	err := appbuild.GradleReleaseBuild(context.Background(), &recordingSummarySink{}, ops, &out, &out, appbuild.GradleReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir, EnableBuildSBOM: false},
		Tasks:               "assemble",
		JavaVersion:         "25",
	})
	if err != nil {
		t.Fatal(err)
	}

	// gradle tasks ran.
	if !strings.Contains(gradleAllArgs(ops.calls), "assemble") {
		t.Errorf("assemble task not run: %s", gradleAllArgs(ops.calls))
	}

	// The wrapper was made executable.
	info, err := os.Stat(filepath.Join(dir, "gradlew"))
	if err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Errorf("gradlew not executable: mode=%v err=%v", info.Mode(), err)
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

	if !strings.Contains(gradleAllArgs(ops.calls), "-x test") {
		t.Errorf("-x test not appended despite --skip-tests: %s", gradleAllArgs(ops.calls))
	}
}

func TestGradleReleaseBuild_MissingWrapperFails(t *testing.T) {
	t.Parallel()

	err := appbuild.GradleReleaseBuild(context.Background(), &recordingSummarySink{}, &recordingGradle{}, io.Discard, io.Discard, appbuild.GradleReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: t.TempDir()},
		Tasks:               "assemble",
	})
	if err == nil || !strings.Contains(err.Error(), "executable") {
		t.Fatalf("err = %v, want missing-gradlew error", err)
	}
}
