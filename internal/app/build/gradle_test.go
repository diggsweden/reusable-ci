// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

type fakeGradle struct {
	args   []string
	dir    string
	runErr error

	// captureInitScript: when set, fake reads the --init-script file's
	// contents into this pointer for the test to inspect.
	captureInitScript *string
}

func (f *fakeGradle) RunInherit(ctx context.Context, _, _ io.Writer, args ...string) error {
	return f.RunInDirInherit(ctx, "", nil, nil, args...)
}

func (f *fakeGradle) RunInDirInherit(_ context.Context, dir string, _, _ io.Writer, args ...string) error {
	f.args = args

	f.dir = dir
	if f.captureInitScript != nil {
		// Read the contents of the init script — must happen before
		// GradleSBOM cleans it up.
		for i, a := range args {
			if a == "--init-script" && i+1 < len(args) { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				body, err := os.ReadFile(args[i+1])
				if err == nil {
					*f.captureInitScript = string(body)
				}

				break
			}
		}
	}

	return f.runErr
}

// projectWithGradlew creates a tempdir containing an executable gradlew
// stub and returns the dir.
func projectWithGradlew(t *testing.T) string {
	t.Helper()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("gradlew", []byte("#!/bin/sh\nexit 0\n"))

	if err := os.Chmod(fsys.Path("gradlew"), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	return fsys.Root
}

func TestGradleSBOM_HappyPath_PassesInitScriptAndCyclonedxBomTask(t *testing.T) {
	dir := projectWithGradlew(t)

	var captured string

	ops := &fakeGradle{captureInitScript: &captured}

	err := appbuild.GradleSBOM(context.Background(), ops, io.Discard, io.Discard, appbuild.GradleSBOMInput{
		CycloneDXVersion: "3.2.1",
		WorkingDir:       dir,
	})
	if err != nil {
		t.Fatalf("GradleSBOM: %v", err)
	}

	if len(ops.args) != 3 || ops.args[0] != "--init-script" || ops.args[2] != "cyclonedxBom" {
		t.Fatalf("unexpected args: %v", ops.args)
	}

	if ops.dir != dir {
		t.Fatalf("dir = %q, want %q", ops.dir, dir)
	}

	// The init script must reference the real plugin artifact, not
	// the plugin-portal marker — see RenderGradleInitScript for the
	// rationale (markers don't resolve in init-script classpath).
	if !strings.Contains(captured, `org.cyclonedx:cyclonedx-gradle-plugin:3.2.1`) {
		t.Errorf("init script missing pinned version:\n%s", captured)
	}
}

func TestGradleSBOM_DeletesInitScriptAfter(t *testing.T) {
	dir := projectWithGradlew(t)

	var initPath string

	captured := func(_ context.Context, _, _ io.Writer, args ...string) error {
		for i, a := range args {
			if a == "--init-script" && i+1 < len(args) {
				initPath = args[i+1]
			}
		}

		return nil
	}
	if err := appbuild.GradleSBOM(context.Background(), runFn(captured), io.Discard, io.Discard, appbuild.GradleSBOMInput{
		CycloneDXVersion: "3.0.0",
		WorkingDir:       dir,
	}); err != nil {
		t.Fatalf("GradleSBOM: %v", err)
	}

	if initPath == "" {
		t.Fatal("did not capture init-script path")
	}

	if _, err := os.Stat(initPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("init script %s still exists after GradleSBOM, expected cleanup", initPath)
	}
}

func TestGradleSBOM_RejectsMissingVersion(t *testing.T) {
	dir := projectWithGradlew(t)
	ops := &fakeGradle{}

	err := appbuild.GradleSBOM(context.Background(), ops, io.Discard, io.Discard, appbuild.GradleSBOMInput{
		WorkingDir: dir,
	})
	if err == nil || !strings.Contains(err.Error(), "CYCLONEDX_GRADLE_VERSION") {
		t.Errorf("expected version-required error, got: %v", err)
	}

	if len(ops.args) != 0 {
		t.Error("did not expect gradlew to be invoked")
	}
}

func TestGradleSBOM_RejectsMissingGradlew(t *testing.T) {
	dir := testfs.NewReal(t).Root // no gradlew

	err := appbuild.GradleSBOM(context.Background(), &fakeGradle{}, io.Discard, io.Discard, appbuild.GradleSBOMInput{
		CycloneDXVersion: "3.0",
		WorkingDir:       dir,
	})
	if err == nil || !strings.Contains(err.Error(), "gradlew") {
		t.Errorf("expected gradlew-not-found error, got: %v", err)
	}
}

func TestGradleSBOM_RejectsNonExecutableGradlew(t *testing.T) {
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.WriteFile("gradlew", []byte("#!/bin/sh\n"))

	err := appbuild.GradleSBOM(context.Background(), &fakeGradle{}, io.Discard, io.Discard, appbuild.GradleSBOMInput{
		CycloneDXVersion: "3.0",
		WorkingDir:       dir,
	})
	if err == nil || !strings.Contains(err.Error(), "executable") {
		t.Errorf("expected non-executable error, got: %v", err)
	}
}

// runFn adapts a closure into the GradleOps interface.
type runFn func(ctx context.Context, out, stderr io.Writer, args ...string) error

func (f runFn) RunInherit(ctx context.Context, out, stderr io.Writer, args ...string) error {
	return f(ctx, out, stderr, args...)
}

func (f runFn) RunInDirInherit(ctx context.Context, _ string, out, stderr io.Writer, args ...string) error {
	return f(ctx, out, stderr, args...)
}

func TestGradleApplication_RunsTasks(t *testing.T) {
	ops := &fakeGradle{}
	if err := appbuild.GradleApplication(context.Background(), ops, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.GradleApplicationInput{
		Tasks: "assemble check",
	}); err != nil {
		t.Fatal(err)
	}

	want := []string{"assemble", "check"} //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	if !equalArgs(ops.args, want) {
		t.Errorf("args = %v, want %v", ops.args, want)
	}
}

func TestGradleApplication_AppendsSkipTests(t *testing.T) {
	ops := &fakeGradle{}
	if err := appbuild.GradleApplication(context.Background(), ops, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.GradleApplicationInput{
		Tasks:     "assemble",
		SkipTests: true,
	}); err != nil {
		t.Fatal(err)
	}

	want := []string{"assemble", "-x", "test"} //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	if !equalArgs(ops.args, want) {
		t.Errorf("args = %v, want %v", ops.args, want)
	}
}

func TestGradleApplication_RejectsEmptyTasks(t *testing.T) {
	err := appbuild.GradleApplication(context.Background(), &fakeGradle{}, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.GradleApplicationInput{})
	if err == nil || !strings.Contains(err.Error(), "at least one gradle task") {
		t.Fatalf("err = %v", err)
	}
}
