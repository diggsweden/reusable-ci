// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

// fakeMaven captures invocations and returns canned EvalExpression results.
type fakeMaven struct {
	answers map[string]string
	evalErr error
	runs    [][]string
	runErr  error
}

func (f *fakeMaven) EvalExpression(_ context.Context, expr string) (string, error) {
	if f.evalErr != nil {
		return "", f.evalErr
	}

	v, ok := f.answers[expr]
	if !ok {
		return "", fmt.Errorf("unexpected expr: %s", expr) //nolint:err113 // test mock error
	}

	return v, nil
}

func (f *fakeMaven) RunInherit(_ context.Context, _, _ io.Writer, args ...string) error {
	f.runs = append(f.runs, args)

	return f.runErr
}

func TestMavenMetadata_WritesAllOutputsAndStatusLineFromPOM(t *testing.T) {
	dir := t.TempDir()

	pomBody := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <groupId>se.digg.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.2.3-SNAPSHOT</version>
</project>`)
	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), pomBody, 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	sink := fakeoutputsink.New(t)
	ops := &fakeMaven{} // not used — pom.xml has no ${} properties

	var out bytes.Buffer
	if err := appbuild.MavenMetadata(context.Background(), sink, ops, &out, appbuild.MavenMetadataInput{Dir: dir}); err != nil {
		t.Fatalf("MavenMetadata: %v", err)
	}

	// A POM with no ${} properties is read directly; mvn is only invoked to
	// interpolate. The check here logged "mvn never invoked" when it had in
	// fact been invoked, and logged rather than failed, so an implementation
	// that shelled out to mvn for every literal POM would have passed
	// silently -- at the cost of a JVM start per metadata read.
	if len(ops.runs) != 0 {
		t.Errorf("mvn invoked %v for a POM needing no interpolation", ops.runs)
	}

	want := map[string]string{
		"version":     "1.2.3-SNAPSHOT",
		"is-snapshot": "true",
		"group-id":    "se.digg.example",
		"artifact-id": "demo", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}
	for k, v := range want {
		if got := sink.Single(k); got != v {
			t.Errorf("output %s = %q, want %q", k, got, v)
		}
	}

	if got := out.String(); got != "Project: se.digg.example:demo:1.2.3-SNAPSHOT\n" {
		t.Errorf("out = %q", got)
	}
}

func TestMavenMetadata_ReleaseVersionIsNotSnapshot(t *testing.T) {
	dir := t.TempDir()

	pomBody := []byte(`<?xml version="1.0"?><project><groupId>g</groupId><artifactId>a</artifactId><version>1.0.0</version></project>`)
	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), pomBody, 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	sink := fakeoutputsink.New(t)
	if err := appbuild.MavenMetadata(context.Background(), sink, &fakeMaven{}, io.Discard, appbuild.MavenMetadataInput{Dir: dir}); err != nil {
		t.Fatalf("MavenMetadata: %v", err)
	}

	if got := sink.Single("is-snapshot"); got != "false" {
		t.Errorf("is-snapshot = %q, want false", got)
	}
}

func TestMavenMetadata_PropertyInterpolationFallsBackToMvn(t *testing.T) {
	dir := t.TempDir()

	pomBody := []byte(`<?xml version="1.0"?>
<project>
  <groupId>g</groupId>
  <artifactId>a</artifactId>
  <version>${revision}</version>
</project>`)
	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), pomBody, 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	sink := fakeoutputsink.New(t)

	ops := &fakeMaven{answers: map[string]string{"project.version": "9.9.9"}} //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	if err := appbuild.MavenMetadata(context.Background(), sink, ops, io.Discard, appbuild.MavenMetadataInput{Dir: dir}); err != nil {
		t.Fatalf("MavenMetadata: %v", err)
	}

	if got := sink.Single("version"); got != "9.9.9" {
		t.Errorf("version = %q, want 9.9.9 (resolved from mvn)", got)
	}

	if got := sink.Single("group-id"); got != "g" {
		t.Errorf("group-id should come from pom.xml literally, got %q", got)
	}
}

func TestMavenMetadata_InheritsFromParent(t *testing.T) {
	dir := t.TempDir()

	pomBody := []byte(`<?xml version="1.0"?>
<project>
  <parent>
    <groupId>se.digg.platform</groupId>
    <artifactId>platform-bom</artifactId>
    <version>2.0.0</version>
  </parent>
  <artifactId>child-module</artifactId>
</project>`)
	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), pomBody, 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	sink := fakeoutputsink.New(t)
	if err := appbuild.MavenMetadata(context.Background(), sink, &fakeMaven{}, io.Discard, appbuild.MavenMetadataInput{Dir: dir}); err != nil {
		t.Fatalf("MavenMetadata: %v", err)
	}

	if got := sink.Single("group-id"); got != "se.digg.platform" {
		t.Errorf("group-id = %q, want inherited 'se.digg.platform'", got)
	}

	if got := sink.Single("version"); got != "2.0.0" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("version = %q, want inherited '2.0.0'", got)
	}

	if got := sink.Single("artifact-id"); got != "child-module" {
		t.Errorf("artifact-id = %q, want literal 'child-module'", got)
	}
}

func TestMavenMetadata_MissingPOMFailsCleanly(t *testing.T) {
	dir := t.TempDir()

	err := appbuild.MavenMetadata(context.Background(), fakeoutputsink.New(t), &fakeMaven{}, io.Discard, appbuild.MavenMetadataInput{Dir: dir})
	if err == nil || !strings.Contains(err.Error(), "pom.xml not found") {
		t.Fatalf("err = %v, want pom.xml-not-found error", err)
	}

	if !errors.Is(err, errs.ErrMissingInput) {
		t.Errorf("err should classify as ErrMissingInput, got %v", err)
	}
}

func TestMavenLibrary_RunsCompileTestPackage_WithProfile(t *testing.T) {
	ops := &fakeMaven{}
	in := appbuild.MavenLibraryInput{
		CLIOpts:   []string{"--batch-mode", "--errors"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Profile:   "central-release",
		SkipTests: false,
	}

	if err := appbuild.MavenLibrary(context.Background(), ops, io.Discard, io.Discard, in); err != nil {
		t.Fatalf("MavenLibrary: %v", err)
	}

	if len(ops.runs) != 3 {
		t.Fatalf("expected 3 mvn invocations, got %d", len(ops.runs))
	}

	wantCompile := []string{"--batch-mode", "--errors", "clean", "compile", "-Pcentral-release"} //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	wantTest := []string{"--batch-mode", "--errors", "test", "-Pcentral-release"}                //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.

	wantPackage := []string{"--batch-mode", "--errors", "package", "-DskipTests=false", "-Pcentral-release", "-Dgpg.skip=true"} //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	for i, want := range [][]string{wantCompile, wantTest, wantPackage} {
		if !equalArgs(ops.runs[i], want) {
			t.Errorf("invocation %d:\n got: %v\nwant: %v", i, ops.runs[i], want)
		}
	}
}

func TestMavenLibrary_SkipTestsTrue_OmitsTestPhase(t *testing.T) {
	ops := &fakeMaven{}

	in := appbuild.MavenLibraryInput{
		CLIOpts:   []string{"-q"},
		SkipTests: true,
	}
	if err := appbuild.MavenLibrary(context.Background(), ops, io.Discard, io.Discard, in); err != nil {
		t.Fatalf("MavenLibrary: %v", err)
	}

	if len(ops.runs) != 2 {
		t.Fatalf("expected 2 mvn invocations (compile + package) got %d: %v", len(ops.runs), ops.runs)
	}

	// Both invocations, exactly, as the profile sibling above already does.
	// Looking for two flags in the package args said nothing about the
	// compile invocation, nor about anything else the package run carries --
	// and the test phase being omitted is the claim in the name.
	wantCompile := []string{"-q", "clean", "compile"}
	wantPackage := []string{"-q", "package", "-DskipTests=true", "-Dgpg.skip=true"}

	for i, want := range [][]string{wantCompile, wantPackage} {
		if !equalArgs(ops.runs[i], want) {
			t.Errorf("invocation %d:\n got: %v\nwant: %v", i, ops.runs[i], want)
		}
	}
}

func TestMavenLibrary_NoProfile_OmitsPFlag(t *testing.T) {
	ops := &fakeMaven{}
	if err := appbuild.MavenLibrary(context.Background(), ops, io.Discard, io.Discard, appbuild.MavenLibraryInput{
		CLIOpts: []string{"-q"},
	}); err != nil {
		t.Fatalf("MavenLibrary: %v", err)
	}

	for _, run := range ops.runs {
		for _, a := range run {
			if strings.HasPrefix(a, "-P") {
				t.Errorf("unexpected -P flag in %v", run)
			}
		}
	}
}

func TestMavenLibrary_BannerLinesGoToStdout(t *testing.T) {
	ops := &fakeMaven{}

	var out bytes.Buffer
	if err := appbuild.MavenLibrary(context.Background(), ops, &out, io.Discard, appbuild.MavenLibraryInput{
		Profile: "central-release",
	}); err != nil {
		t.Fatalf("MavenLibrary: %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"Building Maven library with sources and javadoc...",
		"Using Maven profile: central-release",
		"Running tests...",
		"Creating library package with sources and javadoc...",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("out missing %q\n--- out ---\n%s", want, got)
		}
	}
}

func equalArgs(a, b []string) bool { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] { //nolint:gosec // len(a)==len(b) checked above.
			return false
		}
	}

	return true
}

func TestMavenApplication_AddsSkipTests(t *testing.T) {
	ops := &fakeMaven{}
	if err := appbuild.MavenApplication(context.Background(), ops, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.MavenApplicationInput{
		CLIOpts:   []string{"--batch-mode", "--errors"},
		SkipTests: true,
	}); err != nil {
		t.Fatal(err)
	}

	if len(ops.runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(ops.runs))
	}

	want := []string{"--batch-mode", "--errors", "clean", "package", "-DskipTests"}
	if !equalArgs(ops.runs[0], want) {
		t.Errorf("args = %v, want %v", ops.runs[0], want)
	}
}

func TestMavenApplication_OmitsSkipTestsByDefault(t *testing.T) {
	ops := &fakeMaven{}
	if err := appbuild.MavenApplication(context.Background(), ops, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.MavenApplicationInput{
		CLIOpts: []string{"--batch-mode"},
	}); err != nil {
		t.Fatal(err)
	}

	// equalArgs already says -DskipTests is absent. The separate check that
	// followed could not have found it anyway: the flag is always written
	// -DskipTests=<bool>, so searching for the bare token never matches.
	want := []string{"--batch-mode", "clean", "package"}
	if !equalArgs(ops.runs[0], want) {
		t.Errorf("args = %v, want %v", ops.runs[0], want)
	}
}

func TestMavenApplication_PropagatesError(t *testing.T) {
	ops := &fakeMaven{runErr: errors.New("boom")} //nolint:err113 // test mock error

	err := appbuild.MavenApplication(context.Background(), ops, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.MavenApplicationInput{})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
}
