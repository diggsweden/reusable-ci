// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestXcodeVersionInfo_SelectorsBindToRootNotCWD(t *testing.T) {
	cwd := testfs.NewReal(t)
	t.Chdir(cwd.Root)
	makeXcodeProject(t, cwd, "Sources/Zebra", "8.0", "800")
	cwd.WriteFile("App.xcworkspace/contents.xcworkspacedata", []byte(`<Workspace version="1.0"><FileRef location="group:Sources/Zebra.xcodeproj"/></Workspace>`))

	for _, selector := range []string{"relative project", "absolute project", "relative workspace", "absolute workspace", "nested group", "nested workspace", "XML escaped path", "depth at limit"} {
		t.Run(selector, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			makeXcodeProject(t, fsys, "Alpha", "1.0", "100")
			makeXcodeProject(t, fsys, "App", "3.0", "300") // Workspace basename is not a project selector.
			makeXcodeProject(t, fsys, "Sources/Zebra", "2.0", "200")

			workspace := `<Workspace version="1.0"><FileRef location="group:Sources/Zebra.xcodeproj"/></Workspace>`
			in := appbuild.XcodeVersionInfoInput{Root: fsys.Root}

			switch selector {
			case "relative project":
				in.Project = "Sources/Zebra.xcodeproj"
			case "absolute project":
				in.Project = fsys.Path("Sources/Zebra.xcodeproj")
			case "absolute workspace":
				in.Workspace = fsys.Path("App.xcworkspace")
			case "relative workspace":
				in.Workspace = "App.xcworkspace"
			case "nested group":
				in.Workspace = "App.xcworkspace"
				workspace = `<Workspace version="1.0"><Group location="group:Sources" name="Source Group"><Group location="group:" name="Logical"><FileRef location="group:Zebra.xcodeproj"/></Group></Group></Workspace>`
			case "nested workspace":
				in.Workspace = "Sources/App.xcworkspace"
				workspace = `<Workspace version="1.0"><FileRef location="group:Zebra.xcodeproj"/></Workspace>`
			case "XML escaped path":
				in.Workspace = "App.xcworkspace"

				makeXcodeProject(t, fsys, "Source & App/Zebra", "2.0", "200")

				workspace = `<Workspace version="1.0"><FileRef location="group:Source &amp; App/Zebra.xcodeproj"/></Workspace>`
			case "depth at limit":
				in.Workspace = "App.xcworkspace"
				workspace = `<Workspace>` + strings.Repeat(`<Group location="group:">`, 62) + `<FileRef location="group:Sources/Zebra.xcodeproj"/>` + strings.Repeat(`</Group>`, 62) + `</Workspace>`
			}

			workspacePath := "App.xcworkspace"
			if in.Workspace != "" && !filepath.IsAbs(in.Workspace) {
				workspacePath = in.Workspace
			}

			fsys.WriteFile(filepath.Join(workspacePath, "contents.xcworkspacedata"), []byte(workspace))

			sink := fakeoutputsink.New(t)

			err := appbuild.XcodeVersionInfo(t.Context(), sink, io.Discard, output.Annotator{}, in)
			if err != nil {
				t.Fatal(err)
			}

			if got := sink.AllScalar(); !maps.Equal(got, map[string]string{"version": "2.0", "build": "200"}) {
				t.Fatalf("metadata = %v, want selected Root project 2.0/200", got)
			}
		})
	}
}

func TestXcodeVersionInfo_WorkspacePathSpellingsBindSameProject(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, suffix string
		absolute     bool
	}{
		{"relative canonical", "", false},
		{"relative trailing slash", "/", false},
		{"relative terminal dot", "/.", false},
		{"absolute canonical", "", true},
		{"absolute trailing slash", "/", true},
		{"absolute terminal dot", "/.", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fsys := testfs.NewReal(t)
			makeXcodeProject(t, fsys, "Alpha", "1.0", "100")
			makeXcodeProject(t, fsys, "Sources/Zebra", "2.0", "200")
			makeXcodeProject(t, fsys, "App.xcworkspace/Sources/Zebra", "7.0", "700")
			fsys.WriteFile("App.xcworkspace/contents.xcworkspacedata", []byte(`<Workspace><FileRef location="group:Sources/Zebra.xcodeproj"/></Workspace>`))

			workspace := "App.xcworkspace"
			if tc.absolute {
				workspace = fsys.Path(workspace)
			}

			workspace += tc.suffix // Preserve the caller's spelling, not filepath.Join's cleaned form.
			sink := fakeoutputsink.New(t)

			var stderr strings.Builder

			err := appbuild.XcodeVersionInfo(t.Context(), sink, &stderr, output.Annotator{}, appbuild.XcodeVersionInfoInput{Root: fsys.Root, Workspace: workspace})
			if err != nil {
				t.Fatal(err)
			}

			if got := sink.AllScalar(); !maps.Equal(got, map[string]string{"version": "2.0", "build": "200"}) || stderr.String() != "Version: 2.0 (200)\n" {
				t.Fatalf("metadata = %v stderr=%q, want workspace sibling 2.0/200, not in-bundle decoy", got, &stderr)
			}
		})
	}
}

func TestXcodeVersionInfo_WorkspaceXMLDeclarationPlacement(t *testing.T) {
	t.Parallel()

	const (
		declaration = `<?xml version="1.0" encoding="UTF-8"?>`
		workspace   = `<Workspace><FileRef location="group:Sources/Zebra.xcodeproj"/></Workspace>`
	)
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"no declaration", workspace, true},
		{"first declaration", declaration + "\n" + workspace, true},
		{"BOM declaration", "\xef\xbb\xbf" + declaration + workspace, true},
		{"BOM without declaration", "\xef\xbb\xbf" + workspace, true},
		{"ordinary PIs and comments", `<!--before--><?build fixture?>` + workspace + `<?complete fixture?><!--after-->`, true},
		{"declaration then ordinary PI", declaration + `<?xml-stylesheet href="owned.xsl"?><!--before-->` + workspace + `<!--after--><?complete?>`, true},
		{"declaration after root", workspace + declaration, false},
		{"duplicate declaration", declaration + declaration + workspace, false},
		{"declaration inside root", `<Workspace>` + declaration + `<FileRef location="group:Sources/Zebra.xcodeproj"/></Workspace>`, false},
		{"declaration after comment", `<!--before-->` + declaration + workspace, false},
		{"declaration after ordinary PI", `<?build fixture?>` + declaration + workspace, false},
		{"declaration after whitespace", "\n" + declaration + workspace, false},
		{"uppercase reserved target", `<?XML version="1.0"?>` + workspace, false},
		{"mixed case reserved target", `<?xMl version="1.0"?>` + workspace, false},
		{"BOM then comment then declaration", "\xef\xbb\xbf<!--before-->" + declaration + workspace, false},
		{"duplicate BOM", "\xef\xbb\xbf\xef\xbb\xbf" + declaration + workspace, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fsys := testfs.NewReal(t)
			makeXcodeProject(t, fsys, "Alpha", "1.0", "100")
			makeXcodeProject(t, fsys, "Sources/Zebra", "2.0", "200")
			fsys.WriteFile("App.xcworkspace/contents.xcworkspacedata", []byte(tc.body))

			sink := fakeoutputsink.New(t)

			var stderr strings.Builder

			err := appbuild.XcodeVersionInfo(t.Context(), sink, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appbuild.XcodeVersionInfoInput{Root: fsys.Root, Workspace: "App.xcworkspace"})
			if tc.valid {
				if err != nil || !maps.Equal(sink.AllScalar(), map[string]string{"version": "2.0", "build": "200"}) || stderr.String() != "Version: 2.0 (200)\n" {
					t.Fatalf("valid XML: err=%v outputs=%v stderr=%q, want selected 2.0/200", err, sink.AllScalar(), &stderr)
				}
			} else if !errors.Is(err, errs.ErrValidation) || len(sink.Keys()) != 0 || stderr.Len() != 0 {
				t.Fatalf("invalid declaration: err=%v outputs=%v stderr=%q, want validation refusal without publication", err, sink.AllScalar(), &stderr)
			}
		})
	}
}

func TestXcodeVersionInfo_WorkspaceRefusesUnprovenSelection(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, body string }{
		{"empty", ""},
		{"no refs despite basename project", `<Workspace version="1.0"/>`},
		{"multiple projects", `<Workspace><FileRef location="group:Alpha.xcodeproj"/><FileRef location="group:Sources/Zebra.xcodeproj"/></Workspace>`},
		{"malformed XML", `<Workspace><FileRef location="group:Sources/Zebra.xcodeproj"/></Other>`},
		{"trailing second workspace", `<Workspace><FileRef location="group:Sources/Zebra.xcodeproj"/></Workspace><Workspace/>`},
		{"unknown element", `<Workspace><Unknown><FileRef location="group:Sources/Zebra.xcodeproj"/></Unknown></Workspace>`},
		{"unsupported container", `<Workspace><FileRef location="container:Sources/Zebra.xcodeproj"/></Workspace>`},
		{"unsupported self", `<Workspace><FileRef location="self:Sources/Zebra.xcodeproj"/></Workspace>`},
		{"no location", `<Workspace><FileRef/></Workspace>`},
		{"duplicate location", `<Workspace><FileRef location="group:Alpha.xcodeproj" location="group:Sources/Zebra.xcodeproj"/></Workspace>`},
		{"empty duplicate location", `<Workspace><FileRef location="" location="group:Sources/Zebra.xcodeproj"/></Workspace>`},
		{"nonproject ref", `<Workspace><FileRef location="group:Other.xcworkspace"/></Workspace>`},
		{"escaping ref", `<Workspace><FileRef location="group:../Outside.xcodeproj"/></Workspace>`},
		{"namespaced root", `<Workspace xmlns="urn:unsupported"><FileRef location="group:Sources/Zebra.xcodeproj"/></Workspace>`},
		{"directive", `<!DOCTYPE Workspace><Workspace><FileRef location="group:Sources/Zebra.xcodeproj"/></Workspace>`},
		{"unexpected text", `<Workspace>not a reference<FileRef location="group:Sources/Zebra.xcodeproj"/></Workspace>`},
		{"depth over limit", `<Workspace>` + strings.Repeat(`<Group location="group:">`, 63) + `<FileRef location="group:Sources/Zebra.xcodeproj"/>` + strings.Repeat(`</Group>`, 63) + `</Workspace>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fsys := testfs.NewReal(t)
			makeXcodeProject(t, fsys, "checkout/Alpha", "1.0", "100")
			makeXcodeProject(t, fsys, "checkout/App", "3.0", "300")
			makeXcodeProject(t, fsys, "checkout/Sources/Zebra", "2.0", "200")
			makeXcodeProject(t, fsys, "Outside", "9.0", "900")
			fsys.WriteFile("checkout/App.xcworkspace/contents.xcworkspacedata", []byte(tc.body))

			sink := fakeoutputsink.New(t)

			err := appbuild.XcodeVersionInfo(t.Context(), sink, io.Discard, output.Annotator{}, appbuild.XcodeVersionInfoInput{Root: fsys.Path("checkout"), Workspace: "App.xcworkspace"})
			if !errors.Is(err, errs.ErrValidation) || len(sink.Keys()) != 0 {
				t.Fatalf("err=%v outputs=%v, want validation refusal without publication", err, sink.AllScalar())
			}
		})
	}
}

func TestXcodeVersionInfo_SelectedFilesDoNotFallBack(t *testing.T) { //nolint:gocognit // independent filesystem refusals share only the selected-project/decoy setup and publication assertions.
	t.Parallel()

	for _, tc := range []struct {
		name string
		want error
	}{
		{"explicit missing PBX", nil},
		{"explicit absent keys", nil},
		{"explicit directory PBX", errs.ErrMissingInput},
		{"workspace missing PBX", errs.ErrMissingInput},
		{"workspace directory PBX", errs.ErrMissingInput},
		{"workspace missing descriptor", errs.ErrMissingInput},
		{"workspace directory descriptor", errs.ErrMissingInput},
		{"workspace missing project", errs.ErrMissingInput},
		{"workspace unreadable PBX", errs.ErrPermissionDenied},
		{"symlink workspace", errs.ErrValidation},
		{"symlink project", errs.ErrValidation},
		{"symlink PBX", errs.ErrValidation},
		{"symlink descriptor", errs.ErrValidation},
		{"absolute outside project", errs.ErrValidation},
		{"both selectors", errs.ErrUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fsys := testfs.NewReal(t)
			makeXcodeProject(t, fsys, "checkout/Alpha", "1.0", "100")
			outside := makeXcodeProject(t, fsys, "Outside", "9.0", "900")
			fsys.MkdirAll("checkout/Zebra.xcodeproj")
			descriptor := fsys.WriteFile("checkout/App.xcworkspace/contents.xcworkspacedata", []byte(`<Workspace><FileRef location="group:Zebra.xcodeproj"/></Workspace>`))
			pbx := fsys.Path("checkout/Zebra.xcodeproj/project.pbxproj")

			in := appbuild.XcodeVersionInfoInput{Root: fsys.Path("checkout"), Workspace: "App.xcworkspace"}
			if strings.HasPrefix(tc.name, "explicit") {
				in.Project, in.Workspace = "Zebra.xcodeproj", ""
			}

			switch tc.name {
			case "explicit absent keys":
				fsys.WriteFile("checkout/Zebra.xcodeproj/project.pbxproj", []byte("// No version keys\n"))
			case "explicit directory PBX", "workspace directory PBX":
				fsys.MkdirAll("checkout/Zebra.xcodeproj/project.pbxproj")
			case "workspace missing descriptor", "workspace directory descriptor", "symlink descriptor":
				if err := os.Remove(descriptor); err != nil {
					t.Fatal(err)
				}

				if tc.name == "workspace directory descriptor" {
					fsys.MkdirAll("checkout/App.xcworkspace/contents.xcworkspacedata")
				}

				if tc.name == "symlink descriptor" {
					if err := os.Symlink(fsys.Path("checkout/Alpha.xcodeproj/project.pbxproj"), descriptor); err != nil {
						t.Fatal(err)
					}
				}
			case "workspace missing project":
				if err := os.Remove(fsys.Path("checkout/Zebra.xcodeproj")); err != nil {
					t.Fatal(err)
				}
			case "workspace unreadable PBX":
				if os.Geteuid() == 0 {
					t.Skip("root can read mode-000 files")
				}

				fsys.WriteFile("checkout/Zebra.xcodeproj/project.pbxproj", []byte("unreadable"))

				if err := os.Chmod(pbx, 0); err != nil {
					t.Fatal(err)
				}
			case "symlink workspace":
				if err := os.Symlink("App.xcworkspace", fsys.Path("checkout/Link.xcworkspace")); err != nil {
					t.Fatal(err)
				}

				in.Workspace = "Link.xcworkspace"
			case "symlink project":
				if err := os.Symlink(outside, fsys.Path("checkout/Link.xcodeproj")); err != nil {
					t.Fatal(err)
				}

				in.Project, in.Workspace = "Link.xcodeproj", ""
			case "symlink PBX":
				if err := os.Symlink(fsys.Path("checkout/Alpha.xcodeproj/project.pbxproj"), pbx); err != nil {
					t.Fatal(err)
				}
			case "absolute outside project":
				in.Project, in.Workspace = outside, ""
			case "both selectors":
				in.Project = "Alpha.xcodeproj"
			}

			sink := fakeoutputsink.New(t)

			var stderr strings.Builder

			err := appbuild.XcodeVersionInfo(t.Context(), sink, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), in)
			if tc.want == nil {
				if err != nil || !maps.Equal(sink.AllScalar(), map[string]string{"version": "unknown", "build": "unknown"}) {
					t.Fatalf("err=%v outputs=%v, want explicit unknown without decoy fallback", err, sink.AllScalar())
				}
			} else if !errors.Is(err, tc.want) || len(sink.Keys()) != 0 || stderr.Len() != 0 {
				t.Fatalf("err=%v outputs=%v stderr=%q, want %v without publication", err, sink.AllScalar(), &stderr, tc.want)
			}
		})
	}
}
