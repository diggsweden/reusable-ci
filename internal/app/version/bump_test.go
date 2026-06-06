// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	appversion "github.com/diggsweden/reusable-ci/internal/app/version"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

type fakeMavenOps struct {
	args []string
	dir  string
	err  error
}

func (f *fakeMavenOps) RunInheritIn(_ context.Context, dir string, _, _ io.Writer, args ...string) error {
	f.dir = dir
	f.args = args

	return f.err
}

type fakeNPMOps struct {
	args []string
	dir  string
	err  error
}

func (f *fakeNPMOps) RunInherit(_ context.Context, dir string, _, _ io.Writer, args ...string) error {
	f.dir = dir
	f.args = args

	return f.err
}

type fakeCargoOps struct {
	avail   bool
	calls   [][]string
	failOne bool
	err     error
}

func (f *fakeCargoOps) Available() bool { return f.avail }
func (f *fakeCargoOps) RunInherit(_ context.Context, _ string, _, _ io.Writer, args ...string) error {
	f.calls = append(f.calls, args)
	if f.failOne && len(f.calls) == 1 {
		return errors.New("offline failed") //nolint:err113 // test mock error
	}

	return f.err
}

func TestBump_Maven(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root

	mvn := &fakeMavenOps{}
	if err := appversion.Bump(context.Background(), appversion.BumpOps{Maven: mvn}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType:  projecttype.Maven,
		Version:      "1.2.3", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		WorkingDir:   dir,
		MavenCLIOpts: []string{"--batch-mode"},
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	if mvn.dir != dir {
		t.Errorf("dir = %q, want %q", mvn.dir, dir)
	}

	want := []string{"--batch-mode", "versions:set", "-DnewVersion=1.2.3",
		"-DgenerateBackupPoms=false", "-DprocessAllModules=true", "-DskipTests"}
	if !equalStrings(mvn.args, want) {
		t.Errorf("args = %v, want %v", mvn.args, want)
	}
}

func TestBump_NPM(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root

	npm := &fakeNPMOps{}
	if err := appversion.Bump(context.Background(), appversion.BumpOps{NPM: npm}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.NPM,
		Version:     "1.2.3",
		WorkingDir:  dir,
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	if npm.dir != dir {
		t.Errorf("dir = %q", npm.dir)
	}

	want := []string{"version", "1.2.3", "--no-git-tag-version", "--allow-same-version"}
	if !equalStrings(npm.args, want) {
		t.Errorf("args = %v, want %v", npm.args, want)
	}
}

func TestBump_GradleJVM_RewritesFile(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.WriteFile("gradle.properties", []byte("versionName=ignore\nversion=0.1.0\n"))

	if err := appversion.Bump(context.Background(), appversion.BumpOps{}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.Gradle,
		Version:     "1.0.0", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		WorkingDir:  dir,
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	body := fsys.ReadFile("gradle.properties")
	if !strings.Contains(string(body), "version=1.0.0") {
		t.Errorf("body = %q", body)
	}

	if !strings.Contains(string(body), "versionName=ignore") {
		t.Errorf("must not touch versionName: %q", body)
	}
}

func TestBump_GradleAndroid_RewritesFile(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.WriteFile("gradle.properties", []byte("versionName=0.1.0\nversionCode=42\n"))

	var out bytes.Buffer
	if err := appversion.Bump(context.Background(), appversion.BumpOps{}, &out, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.GradleAndroid,
		Version:     "1.0.0",
		WorkingDir:  dir,
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	body := fsys.ReadFile("gradle.properties")
	if !strings.Contains(string(body), "versionName=1.0.0") {
		t.Errorf("missing versionName=1.0.0: %q", body)
	}

	if !strings.Contains(string(body), "versionCode=43") {
		t.Errorf("missing versionCode=43: %q", body)
	}

	if !strings.Contains(out.String(), "Incremented versionCode: 42 → 43") {
		t.Errorf("missing log: %s", out.String())
	}
}

func TestBump_GradleAndroid_NoVersionCodeAddsOne(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.WriteFile("gradle.properties", []byte(""))

	if err := appversion.Bump(context.Background(), appversion.BumpOps{}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.GradleAndroid,
		Version:     "1.0.0",
		WorkingDir:  dir,
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	body := fsys.ReadFile("gradle.properties")
	for _, want := range []string{"versionName=1.0.0", "versionCode=1"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("missing %s in: %q", want, body)
		}
	}
}

func TestBump_GradleJVM_FileMissingErrors(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)

	var stderr bytes.Buffer

	err := appversion.Bump(context.Background(), appversion.BumpOps{}, io.Discard, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appversion.BumpInput{
		ProjectType: projecttype.Gradle,
		Version:     "1.0.0",
		WorkingDir:  fsys.Root,
	})
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(stderr.String(), "::error::Gradle version file not found") {
		t.Errorf("expected ::error:: line, got: %s", stderr.String())
	}
}

func TestBump_XcodeIOS_CreatesFileWhenMissing(t *testing.T) {
	t.Parallel()

	fsys := testfs.NewReal(t)
	if err := appversion.Bump(context.Background(), appversion.BumpOps{}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType:  projecttype.XcodeIOS,
		Version:      "1.2.3",
		WorkingDir:   fsys.Root,
		XcconfigFile: "versions.xcconfig",
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	body := fsys.ReadFile("versions.xcconfig")
	if string(body) != "MARKETING_VERSION = 1.2.3\n" {
		t.Errorf("body = %q", body)
	}
}

func TestBump_XcodeIOS_UpdatesExisting(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("versions.xcconfig", []byte("MARKETING_VERSION = 0.0.1\nFOO = bar\n"))

	if err := appversion.Bump(context.Background(), appversion.BumpOps{}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.XcodeIOS,
		Version:     "1.2.3",
		WorkingDir:  fsys.Root,
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	body := fsys.ReadFile("versions.xcconfig")
	if !strings.Contains(string(body), "MARKETING_VERSION = 1.2.3") {
		t.Errorf("body = %q", body)
	}

	if !strings.Contains(string(body), "FOO = bar") {
		t.Errorf("must not touch other keys: %q", body)
	}
}

func TestBump_Cargo_Workspace(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	cargo := `[workspace.package]
version = "0.0.1"
edition = "2024"

[dependencies]
serde = "1"
`
	fsys.WriteFile("Cargo.toml", []byte(cargo))

	ops := &fakeCargoOps{avail: true}
	if err := appversion.Bump(context.Background(), appversion.BumpOps{Cargo: ops}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.Cargo,
		Version:     "1.0.0",
		WorkingDir:  dir,
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	body := fsys.ReadFile("Cargo.toml")
	if !strings.Contains(string(body), `version = "1.0.0"`) {
		t.Errorf("body = %s", body)
	}
}

func TestBump_Cargo_RefreshesLockWhenPresent(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	cargoToml := `[package]
name = "demo"
version = "0.0.1"
`
	fsys.WriteFile("Cargo.toml", []byte(cargoToml))
	fsys.WriteFile("Cargo.lock", []byte("# stub"))

	ops := &fakeCargoOps{avail: true}
	if err := appversion.Bump(context.Background(), appversion.BumpOps{Cargo: ops}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.Cargo,
		Version:     "1.0.0",
		WorkingDir:  dir,
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	if len(ops.calls) == 0 {
		t.Fatal("expected cargo update to be invoked")
	}

	want := []string{"update", "--workspace", "--offline"}
	if !equalStrings(ops.calls[0], want) {
		t.Errorf("first cargo call = %v, want %v", ops.calls[0], want)
	}
}

func TestBump_Cargo_OfflineFailureFallsBackToOnline(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.WriteFile("Cargo.toml", []byte("[package]\nname = \"x\"\nversion = \"0.0.1\"\n"))
	fsys.WriteFile("Cargo.lock", []byte("# stub"))

	ops := &fakeCargoOps{avail: true, failOne: true}
	if err := appversion.Bump(context.Background(), appversion.BumpOps{Cargo: ops}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.Cargo,
		Version:     "1.0.0",
		WorkingDir:  dir,
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	if len(ops.calls) != 2 {
		t.Errorf("expected 2 cargo calls (offline then online), got %d", len(ops.calls))
	}
}

func TestBump_Cargo_NoLockSkipsRefresh(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.WriteFile("Cargo.toml", []byte("[package]\nname = \"x\"\nversion = \"0.0.1\"\n"))

	ops := &fakeCargoOps{avail: true}
	if err := appversion.Bump(context.Background(), appversion.BumpOps{Cargo: ops}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.Cargo,
		Version:     "1.0.0",
		WorkingDir:  dir,
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	if len(ops.calls) != 0 {
		t.Errorf("expected no cargo calls when Cargo.lock missing, got %d", len(ops.calls))
	}
}

func TestBump_Cargo_NotInstalledWarnsButSucceeds(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.WriteFile("Cargo.toml", []byte("[package]\nname = \"x\"\nversion = \"0.0.1\"\n"))
	fsys.WriteFile("Cargo.lock", []byte("# stub"))

	ops := &fakeCargoOps{avail: false}

	var out bytes.Buffer
	if err := appversion.Bump(context.Background(), appversion.BumpOps{Cargo: ops}, &out, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.Cargo,
		Version:     "1.0.0",
		WorkingDir:  dir,
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	if !strings.Contains(out.String(), "Warning: cargo not found") {
		t.Errorf("expected warning, got: %s", out.String())
	}
}

func TestBump_Meta_NoOp(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	if err := appversion.Bump(context.Background(), appversion.BumpOps{}, &out, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.Meta,
		Version:     "1.2.3",
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	if !strings.Contains(out.String(), "Meta project type") {
		t.Errorf("expected meta message:\n%s", out.String())
	}
}

func TestBump_Go_NoOp(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	if err := appversion.Bump(context.Background(), appversion.BumpOps{}, &out, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.Go,
		Version:     "1.2.3",
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	if !strings.Contains(out.String(), "Go project type") || !strings.Contains(out.String(), "1.2.3") {
		t.Errorf("expected go no-op message:\n%s", out.String())
	}
}

func TestBump_UnknownTypeErrors(t *testing.T) {
	t.Parallel()

	err := appversion.Bump(context.Background(), appversion.BumpOps{}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: "java",
		Version:     "1.0.0",
	})
	if err == nil || !strings.Contains(err.Error(), "unknown project type") {
		t.Errorf("expected unknown-type error, got: %v", err)
	}
}

func TestBump_RequiresVersion(t *testing.T) {
	t.Parallel()

	if err := appversion.Bump(context.Background(), appversion.BumpOps{}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.Maven,
	}); err == nil {
		t.Fatal("expected error")
	}
}

func equalStrings(a, b []string) bool { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
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
