// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
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
		return "", fmt.Errorf("unexpected expr: %s", expr)
	}
	return v, nil
}

func (f *fakeMaven) RunInherit(_ context.Context, _, _ io.Writer, args ...string) error {
	f.runs = append(f.runs, args)
	return f.runErr
}

func TestMavenMetadata_WritesAllOutputsAndStatusLine(t *testing.T) {
	sink := fakeoutputsink.New(t)
	ops := &fakeMaven{answers: map[string]string{
		"project.version":    "1.2.3-SNAPSHOT",
		"project.groupId":    "se.digg.example",
		"project.artifactId": "demo",
	}}
	var stdout bytes.Buffer

	if err := appbuild.MavenMetadata(context.Background(), sink, ops, &stdout); err != nil {
		t.Fatalf("MavenMetadata: %v", err)
	}

	want := map[string]string{
		"VERSION":     "1.2.3-SNAPSHOT",
		"IS_SNAPSHOT": "true",
		"GROUP_ID":    "se.digg.example",
		"ARTIFACT_ID": "demo",
	}
	for k, v := range want {
		if got := sink.Single(k); got != v {
			t.Errorf("output %s = %q, want %q", k, got, v)
		}
	}

	if got := stdout.String(); got != "Project: se.digg.example:demo:1.2.3-SNAPSHOT\n" {
		t.Errorf("stdout = %q", got)
	}
}

func TestMavenMetadata_ReleaseVersionIsNotSnapshot(t *testing.T) {
	sink := fakeoutputsink.New(t)
	ops := &fakeMaven{answers: map[string]string{
		"project.version":    "1.0.0",
		"project.groupId":    "g",
		"project.artifactId": "a",
	}}
	if err := appbuild.MavenMetadata(context.Background(), sink, ops, io.Discard); err != nil {
		t.Fatalf("MavenMetadata: %v", err)
	}
	if got := sink.Single("IS_SNAPSHOT"); got != "false" {
		t.Errorf("IS_SNAPSHOT = %q, want false", got)
	}
}

func TestMavenLibrary_RunsCompileTestPackage_WithProfile(t *testing.T) {
	ops := &fakeMaven{}
	in := appbuild.MavenLibraryInput{
		CLIOpts:   []string{"--batch-mode", "--errors"},
		Profile:   "central-release",
		SkipTests: false,
	}

	if err := appbuild.MavenLibrary(context.Background(), ops, io.Discard, io.Discard, in); err != nil {
		t.Fatalf("MavenLibrary: %v", err)
	}

	if len(ops.runs) != 3 {
		t.Fatalf("expected 3 mvn invocations, got %d", len(ops.runs))
	}
	wantCompile := []string{"--batch-mode", "--errors", "clean", "compile", "-Pcentral-release"}
	wantTest := []string{"--batch-mode", "--errors", "test", "-Pcentral-release"}
	wantPackage := []string{"--batch-mode", "--errors", "package", "-DskipTests=false", "-Pcentral-release", "-Dgpg.skip=true"}
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
	pkg := ops.runs[1]
	if !contains(pkg, "-DskipTests=true") {
		t.Errorf("package args missing -DskipTests=true: %v", pkg)
	}
	if !contains(pkg, "-Dgpg.skip=true") {
		t.Errorf("package args missing -Dgpg.skip=true: %v", pkg)
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
	var stdout bytes.Buffer
	if err := appbuild.MavenLibrary(context.Background(), ops, &stdout, io.Discard, appbuild.MavenLibraryInput{
		Profile: "central-release",
	}); err != nil {
		t.Fatalf("MavenLibrary: %v", err)
	}
	got := stdout.String()
	for _, want := range []string{
		"Building Maven library with sources and javadoc...",
		"Using Maven profile: central-release",
		"Running tests...",
		"Creating library package with sources and javadoc...",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stdout missing %q\n--- stdout ---\n%s", want, got)
		}
	}
}

func equalArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}
