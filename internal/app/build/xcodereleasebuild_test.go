// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"context"
	"errors"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

type unsignedXcodeSecurity struct{ t *testing.T }

func (sec unsignedXcodeSecurity) Run(context.Context, ...string) (string, error) {
	sec.t.Helper()
	sec.t.Fatal("unsigned release invoked security")

	return "", nil
}

func TestXcodeReleaseBuild_UnsignedSelectedMetadata(t *testing.T) {
	for _, selector := range []string{"project", "workspace"} {
		t.Run(selector, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			t.Chdir(fsys.Root)
			makeXcodeProject(t, fsys, "Alpha", "1.0", "100")
			makeXcodeProject(t, fsys, "Sources/Zebra", "2.0", "200")
			fsys.WriteFile("App.xcworkspace/contents.xcworkspacedata", []byte(`<?xml version="1.0" encoding="UTF-8"?>
<Workspace version="1.0"><FileRef location="group:Sources/Zebra.xcodeproj"></FileRef></Workspace>`))

			in := appbuild.XcodeReleaseBuildInput{
				ArtifactName: "mobile", RepositoryName: "ignored", IncludeTag: true, RefName: "v2.0",
				Scheme: "Zebra", Configuration: "Release", Destination: "generic/platform=iOS",
				BuildNumber: "999", // Archive override must not replace source metadata.
			}

			flag, identity := "-project", "Sources/Zebra.xcodeproj"
			if selector == "workspace" {
				flag, identity = "-workspace", "App.xcworkspace"
				in.Workspace = identity
			} else {
				in.Project = identity
			}

			buildOps := &fakeXcodeBuild{run: func(args []string) {
				if len(args) < 9 || args[0] != "archive" || args[7] != "-archivePath" || args[8] != "build/app.xcarchive" {
					t.Fatalf("unexpected archive invocation: %q", args)
				}

				fsys.WriteFile("build/app.xcarchive/Info.plist", []byte("owned fake archive"))
			}}
			sink := fakeoutputsink.New(t)

			var out, stderr strings.Builder

			err := appbuild.XcodeReleaseBuild(t.Context(), sink, unsignedXcodeSecurity{t}, buildOps, output.NewAnnotator(&stderr, output.FormatGitHub), &out, &stderr, in)
			if err != nil {
				t.Fatal(err)
			}

			wantOutputs := map[string]string{"ipa-name": "mobile-v2.0", "version": "2.0", "build": "200"}
			if got := sink.AllScalar(); !maps.Equal(got, wantOutputs) {
				t.Errorf("outputs = %v, want %v", got, wantOutputs)
			}

			wantArchive := []string{
				"archive", flag, identity, "-scheme", "Zebra", "-configuration", "Release",
				"-archivePath", "build/app.xcarchive", "-destination", "generic/platform=iOS",
				"-skipPackagePluginValidation", "CURRENT_PROJECT_VERSION=999",
			}
			if len(buildOps.calls) != 1 || !slices.Equal(buildOps.calls[0], wantArchive) {
				t.Errorf("xcodebuild calls = %q, want only %q", buildOps.calls, wantArchive)
			}

			if stderr.String() != "Version: 2.0 (200)\n" || !strings.Contains(out.String(), "Built artifacts:\nbuild/app.xcarchive\n") {
				t.Errorf("stderr=%q out=%q", &stderr, &out)
			}

			body, err := os.ReadFile(fsys.Path("build/app.xcarchive/Info.plist"))
			if err != nil || string(body) != "owned fake archive" {
				t.Fatalf("fake archive = %q, err=%v", body, err)
			}
		})
	}
}

func TestXcodeReleaseBuild_UnsignedSelectionRefusalPublishesNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"ambiguous workspace", errs.ErrValidation},
		{"empty workspace", errs.ErrValidation},
		{"missing descriptor", errs.ErrMissingInput},
		{"missing PBX", errs.ErrMissingInput},
		{"directory PBX", errs.ErrMissingInput},
		{"explicit directory PBX", errs.ErrMissingInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			t.Chdir(fsys.Root)
			makeXcodeProject(t, fsys, "Alpha", "1.0", "100")
			makeXcodeProject(t, fsys, "App", "3.0", "300")
			fsys.MkdirAll("Sources/Zebra.xcodeproj")
			fsys.MkdirAll("App.xcworkspace")

			workspace := `<Workspace><FileRef location="group:Sources/Zebra.xcodeproj"/></Workspace>`

			switch tc.name {
			case "ambiguous workspace":
				makeXcodeProject(t, fsys, "Sources/Zebra", "2.0", "200")

				workspace = `<Workspace><FileRef location="group:Alpha.xcodeproj"/><FileRef location="group:Sources/Zebra.xcodeproj"/></Workspace>`
			case "empty workspace":
				workspace = `<Workspace/>`
			case "directory PBX", "explicit directory PBX":
				fsys.MkdirAll("Sources/Zebra.xcodeproj/project.pbxproj")
			}

			if tc.name != "missing descriptor" {
				fsys.WriteFile("App.xcworkspace/contents.xcworkspacedata", []byte(workspace))
			}

			in := appbuild.XcodeReleaseBuildInput{
				RepositoryName: "app", Workspace: "App.xcworkspace", Scheme: "Zebra",
				Configuration: "Release", Destination: "generic/platform=iOS",
			}
			if tc.name == "explicit directory PBX" {
				in.Project, in.Workspace = "Sources/Zebra.xcodeproj", ""
			}

			ops := &fakeXcodeBuild{}
			sink := fakeoutputsink.New(t)

			var out, stderr strings.Builder

			err := appbuild.XcodeReleaseBuild(t.Context(), sink, unsignedXcodeSecurity{t}, ops, output.NewAnnotator(&stderr, output.FormatGitHub), &out, &stderr, in)
			if !errors.Is(err, tc.want) || !strings.Contains(err.Error(), "resolve version") {
				t.Fatalf("err=%v, want metadata refusal classified %v", err, tc.want)
			}

			if len(ops.calls) != 0 || len(sink.Keys()) != 0 || stderr.Len() != 0 || strings.Contains(out.String(), "Built artifacts:") {
				t.Fatalf("refusal calls=%v outputs=%v stderr=%q out=%q", ops.calls, sink.AllScalar(), &stderr, &out)
			}

			if _, err := os.Stat(fsys.Path("build")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("refusal created build directory: %v", err)
			}

			if got := string(fsys.ReadFile("Alpha.xcodeproj/project.pbxproj")); got != "MARKETING_VERSION = 1.0;\nCURRENT_PROJECT_VERSION = 100;\n" {
				t.Fatalf("refusal changed decoy source: %q", got)
			}
		})
	}
}

func TestXcodeReleaseBuild_UnsignedWorkspaceSpellingsKeepArchiveRefusal(t *testing.T) {
	for _, spelling := range []string{"relative slash", "relative dot", "absolute slash", "absolute dot"} {
		t.Run(spelling, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			t.Chdir(fsys.Root)
			makeXcodeProject(t, fsys, "Sources/Zebra", "2.0", "200")
			makeXcodeProject(t, fsys, "App.xcworkspace/Sources/Zebra", "7.0", "700")
			fsys.WriteFile("App.xcworkspace/contents.xcworkspacedata", []byte(`<Workspace><FileRef location="group:Sources/Zebra.xcodeproj"/></Workspace>`))

			workspace := "App.xcworkspace"
			if strings.HasPrefix(spelling, "absolute") {
				workspace = fsys.Path(workspace)
			}

			workspace += "/"
			if strings.HasSuffix(spelling, "dot") {
				workspace += "."
			}

			ops := &fakeXcodeBuild{}
			sink := fakeoutputsink.New(t)
			err := appbuild.XcodeReleaseBuild(t.Context(), sink, unsignedXcodeSecurity{t}, ops, output.Annotator{}, io.Discard, io.Discard, appbuild.XcodeReleaseBuildInput{
				RepositoryName: "app", Workspace: workspace, Scheme: "Zebra",
				Configuration: "Release", Destination: "generic/platform=iOS",
			})
			// Standalone metadata normalizes paths; archive still validates the raw selector.
			if !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), "archive: xcode identity") {
				t.Fatalf("err=%v, want raw archive selector refusal", err)
			}

			if len(ops.calls) != 0 || len(sink.Keys()) != 0 {
				t.Fatalf("refused archive calls=%v outputs=%v", ops.calls, sink.AllScalar())
			}

			if _, err := os.Stat(fsys.Path("build")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("refusal created build directory: %v", err)
			}
		})
	}
}

func TestXcodeReleaseBuild_ArchiveErrorPropagates(t *testing.T) {
	t.Chdir(t.TempDir())

	ops := &fakeXcodeBuild{}
	sink := fakeoutputsink.New(t)

	err := appbuild.XcodeReleaseBuild(context.Background(), sink, &fakeSecurity{}, ops, output.Annotator{}, io.Discard, io.Discard, appbuild.XcodeReleaseBuildInput{
		RepositoryName:    "app",
		EnableCodeSigning: false,
		// no scheme/configuration/destination → archive fails
	})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	// Refused before xcodebuild is reached.
	if len(ops.calls) != 0 {
		t.Errorf("ran xcodebuild without a scheme: %v", ops.calls)
	}

	if got := sink.Keys(); len(got) != 0 {
		t.Errorf("outputs on a failed archive = %v, want none", got)
	}
}
