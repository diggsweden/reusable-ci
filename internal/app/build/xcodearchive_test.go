// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

type fakeXcodeBuild struct {
	calls    [][]string
	exitCode int
	err      error
}

func (f *fakeXcodeBuild) RunInherit(_ context.Context, _, _ io.Writer, args ...string) (int, error) {
	f.calls = append(f.calls, args)
	if f.err != nil {
		return -1, f.err
	}

	return f.exitCode, nil
}

func TestXcodeArchive_WorkspaceArgComposition(t *testing.T) {
	fsys := testfs.NewReal(t)
	tmp := fsys.Root
	fsys.Chdir()

	ops := &fakeXcodeBuild{}

	var out bytes.Buffer
	if err := appbuild.XcodeArchive(context.Background(), ops, &out, io.Discard, appbuild.XcodeArchiveInput{
		Workspace:     "App.xcworkspace",
		Scheme:        "App",                  //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Configuration: "Release",              //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Destination:   "generic/platform=iOS", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		XcconfigPath:  "Config.xcconfig",
		BuildNumber:   "42",
	}); err != nil {
		t.Fatal(err)
	}

	if len(ops.calls) != 1 {
		t.Fatalf("expected 1 xcodebuild call, got %d", len(ops.calls))
	}

	// The whole invocation, in order. Membership checks proved the flag and
	// its value were both somewhere in the argv, which is not the same as
	// their being a pair -- the comment claimed a pair and nothing said so.
	want := []string{
		"archive",
		"-workspace", "App.xcworkspace",
		"-scheme", "App",
		"-configuration", "Release",
		"-archivePath", "build/app.xcarchive",
		"-destination", "generic/platform=iOS",
		"-skipPackagePluginValidation",
		"-xcconfig", "Config.xcconfig",
		"CURRENT_PROJECT_VERSION=42",
	}
	if !reflect.DeepEqual(ops.calls[0], want) {
		t.Errorf("xcodebuild args =\n%q\nwant\n%q", ops.calls[0], want)
	}

	for _, want := range []string{
		"Running: xcodebuild archive -workspace App.xcworkspace",
		"-xcconfig Config.xcconfig",
		"CURRENT_PROJECT_VERSION=42",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("out missing %q:\n%s", want, out.String())
		}
	}
	// build/ created.
	if _, err := os.Stat(filepath.Join(tmp, "build")); err != nil {
		t.Errorf("build/ not created: %v", err)
	}
}

func TestXcodeArchive_ProjectFallback(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	ops := &fakeXcodeBuild{}

	var out bytes.Buffer
	if err := appbuild.XcodeArchive(context.Background(), ops, &out, io.Discard, appbuild.XcodeArchiveInput{
		Project: "App.xcodeproj", Scheme: "App",
		Configuration: "Release", Destination: "generic/platform=iOS",
	}); err != nil {
		t.Fatal(err)
	}

	// With no workspace, the project is used instead — and nothing else
	// changes. Comparing the invocation says both, including that
	// -workspace is absent, which a membership check needed its own
	// assertion for.
	want := []string{
		"archive",
		"-project", "App.xcodeproj",
		"-scheme", "App",
		"-configuration", "Release",
		"-archivePath", "build/app.xcarchive",
		"-destination", "generic/platform=iOS",
		"-skipPackagePluginValidation",
	}
	if !reflect.DeepEqual(ops.calls[0], want) {
		t.Errorf("xcodebuild args =\n%q\nwant\n%q", ops.calls[0], want)
	}

	if !strings.Contains(out.String(), "Running: xcodebuild archive -project App.xcodeproj") {
		t.Errorf("out = %q", out.String())
	}
}

func TestXcodeArchive_RequiresSchemeConfigurationDestination(t *testing.T) {
	cases := []appbuild.XcodeArchiveInput{
		{Configuration: "Release", Destination: "generic/platform=iOS"}, // no scheme
		{Scheme: "App", Destination: "generic/platform=iOS"},            // no config
		{Scheme: "App", Configuration: "Release"},                       // no destination
	}
	for i, c := range cases {
		if err := appbuild.XcodeArchive(context.Background(), &fakeXcodeBuild{}, io.Discard, io.Discard, c); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}
