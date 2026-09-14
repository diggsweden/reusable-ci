// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func ownedTree(t *testing.T, root string) map[string]string {
	t.Helper()

	state := map[string]string{}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		var body []byte
		if info.Mode().IsRegular() {
			body, err = os.ReadFile(path) //nolint:gosec // owned fixture only; this snapshots state, never follows entries classified as links.
		} else if info.Mode()&fs.ModeSymlink != 0 {
			var target string

			target, err = os.Readlink(path)
			body = []byte(target)
		}

		state[strings.TrimPrefix(path, root)] = fmt.Sprintf("%v:%s", info.Mode(), body)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return state
}

func TestGoForwardingBoundary_ExactReleaseInputs(t *testing.T) { //nolint:gocognit // two precedence cases assert every field of the complete four-call sequence.
	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")

	for _, override := range []bool{true, false} {
		t.Run(strconv.FormatBool(override), func(t *testing.T) {
			dir := newGoModDir(t)
			tool, sbom := &fakeGoTool{}, &fakeCycloneDXGoModTool{}

			var stdout, stderr bytes.Buffer

			in := appbuild.GoReleaseBuildInput{
				ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir, ArtifactName: "release-bundle", EnableBuildSBOM: true},
				BinaryName:          "executable", Version: "v2.3.4", RefName: "v9.8.7", Commit: "abc123def456",
				Platforms: "linux/amd64,windows/arm64", BuildTags: "production,netgo", LDFlags: "-X main.channel=stable", MainPackage: "./cmd/service",
			}
			version, binary := "2.3.4", "executable"

			if !override {
				in.Version, in.BinaryName = "", ""
				version, binary = "9.8.7", "release-bundle"
			}

			if err := appbuild.GoReleaseBuild(t.Context(), &recordingSummarySink{}, tool, sbom, &stdout, &stderr, in); err != nil {
				t.Fatal(err)
			}

			want := []appbuild.GoRunInput{ //nolint:prealloc // literal keeps the two non-build calls readable beside the matrix additions.
				{Dir: dir, Args: []string{"mod", "download"}},
				{Dir: dir, Args: []string{"test", "-tags", "production,netgo", "./..."}},
			}
			for _, target := range []struct{ os, arch, suffix string }{{"linux", "amd64", ""}, {"windows", "arm64", ".exe"}} {
				want = append(want, appbuild.GoRunInput{Dir: dir,
					Env:  []string{"CGO_ENABLED=0", "GOOS=" + target.os, "GOARCH=" + target.arch},
					Args: []string{"build", "-trimpath", "-buildvcs=false", "-ldflags", "-s -w -X main.version=" + version + " -X main.commit=abc123def456 -X main.date=2023-11-14T22:13:20Z -X main.channel=stable", "-tags", "production,netgo", "-o", filepath.Join(dir, "dist", target.os+"-"+target.arch, binary+"-"+target.os+"-"+target.arch+target.suffix), "./cmd/service"},
				})
			}

			if len(tool.calls) != len(want) {
				t.Fatalf("calls=%v", tool.calls)
			}

			for idx, got := range tool.calls {
				if got.Stdout != &stdout || got.Stderr != &stderr {
					t.Fatalf("call %d writers not forwarded", idx)
				}

				got.Stdout, got.Stderr = nil, nil
				if !reflect.DeepEqual(got, want[idx]) {
					t.Errorf("call %d=%+v, want %+v", idx, got, want[idx])
				}
			}

			if len(sbom.calls) != 1 {
				t.Fatalf("SBOM calls=%v", sbom.calls)
			}

			got := sbom.calls[0]
			if got.Dir != dir || len(got.Env) != 0 || got.Stdout != &stdout || got.Stderr != &stderr ||
				!slices.Equal(got.Args, []string{"mod", "-json", "-output", ".reusable-ci/go-build-sbom/release-bundle/bom.json", "."}) {
				t.Fatalf("SBOM input=%+v", got)
			}
		})
	}
}

func TestGoRefusalBoundary_PreservesOwnedTree(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*appbuild.GoBuildBinariesInput)
		epoch  string
	}{
		{name: "epoch", epoch: "yesterday"}, {name: "epoch overflow", epoch: "9223372036854775807"},
		{name: "version", change: func(in *appbuild.GoBuildBinariesInput) { in.Version = "1\n2" }},
		{name: "missing version", change: func(in *appbuild.GoBuildBinariesInput) { in.Version = "" }},
		{name: "binary", change: func(in *appbuild.GoBuildBinariesInput) { in.BinaryName = "../outside" }},
		{name: "commit", change: func(in *appbuild.GoBuildBinariesInput) { in.Commit = "a\nb" }},
		{name: "tags", change: func(in *appbuild.GoBuildBinariesInput) { in.BuildTags = "a\x1bb" }},
		{name: "ldflags", change: func(in *appbuild.GoBuildBinariesInput) { in.LDFlags = "a\x00b" }},
		{name: "package", change: func(in *appbuild.GoBuildBinariesInput) { in.MainPackage = "a\xffb" }},
		{name: "late platform", change: func(in *appbuild.GoBuildBinariesInput) { in.Platforms = "linux/amd64,other/bogus" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SOURCE_DATE_EPOCH", tc.epoch)
			fsys := testfs.NewReal(t)
			fsys.WriteFile("dist/linux-amd64/prior", []byte("keep executable bytes"))
			fsys.WriteFile("dist/sibling.tgz", []byte("keep sibling"))
			before := ownedTree(t, fsys.Root)

			in := appbuild.GoBuildBinariesInput{Dir: fsys.Root, BinaryName: "app", Version: "1.0.0", Platforms: "linux/amd64"}
			if tc.change != nil {
				tc.change(&in)
			}

			tool := &fakeGoTool{}

			var out bytes.Buffer

			err := appbuild.GoBuildBinaries(t.Context(), tool, &out, &out, in)
			if !errors.Is(err, errs.ErrUsage) || len(tool.calls) != 0 || out.Len() != 0 {
				t.Fatalf("err=%v calls=%v out=%s", err, tool.calls, &out)
			}

			if !reflect.DeepEqual(before, ownedTree(t, fsys.Root)) {
				t.Fatal("refusal changed owned tree")
			}
		})
	}
}

func TestSwiftDirectoryBoundary_RequestedProjectAndErrors(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile(".swiftlint.yml", []byte("cwd decoy"))
	fsys.WriteFile("sibling/.swiftlint.yml", []byte("sibling decoy"))
	dir := fsys.MkdirAll("nested", "project")
	files := &fakeFiles{listed: "Sources/Selected.swift\n"}

	format := &fakeSwiftFormat{}
	if err := appbuild.SwiftFormatLint(t.Context(), files, format, &recordingSummarySink{}, io.Discard, io.Discard, output.Annotator{}, appbuild.SwiftFormatLintInput{Dir: dir}); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(files.args, [][]string{{"-C", dir, "ls-files", "--", "*.swift"}}) ||
		!slices.Equal(format.dirs, []string{dir}) || !reflect.DeepEqual(format.files, [][]string{{"Sources/Selected.swift"}}) {
		t.Fatalf("listing=%v dirs=%v files=%v", files.args, format.dirs, format.files)
	}

	for _, present := range []bool{false, true} {
		if present {
			fsys.WriteFile("nested/project/.swiftlint.yml", []byte("selected"))
		}

		lint := &fakeSwiftLint{}
		if err := appbuild.SwiftLintLint(t.Context(), lint, &recordingSummarySink{}, io.Discard, io.Discard, output.Annotator{}, appbuild.SwiftLintLintInput{Dir: dir, ConfigPath: ".swiftlint.yml"}); err != nil {
			t.Fatal(err)
		}

		want := ""
		if present {
			want = ".swiftlint.yml"
		}

		if !reflect.DeepEqual(lint.inputs, []appbuild.SwiftLintRunInput{{Dir: dir, ConfigPath: want}}) {
			t.Fatalf("lint=%v", lint.inputs)
		}
	}

	for _, stage := range []string{"listing", "format", "lint"} {
		t.Run(stage, func(t *testing.T) {
			cause := errors.New(stage + " sentinel") //nolint:err113 // a distinct per-stage sentinel proves dependency error identity.
			files, format, lint := &fakeFiles{listed: "Selected.swift"}, &fakeSwiftFormat{}, &fakeSwiftLint{}
			summary := &recordingSummarySink{}

			var err error

			switch stage {
			case "listing":
				files.err = cause
			case "format":
				format.err = cause
			case "lint":
				lint.err = cause
			}

			if stage == "lint" {
				err = appbuild.SwiftLintLint(t.Context(), lint, summary, io.Discard, io.Discard, output.Annotator{}, appbuild.SwiftLintLintInput{Dir: dir})
			} else {
				err = appbuild.SwiftFormatLint(t.Context(), files, format, summary, io.Discard, io.Discard, output.Annotator{}, appbuild.SwiftFormatLintInput{Dir: dir})
			}

			if !errors.Is(err, cause) || summary.buf.Len() != 0 || (stage == "listing" && len(format.files) != 0) {
				t.Fatalf("err=%v summary=%s format=%v", err, &summary.buf, format.files)
			}
		})
	}
}

func TestCargoSelectionBoundary_ExactSourceAndAmbiguousRefusal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tool := &fakeCargoTool{binaryNames: []string{"selected", "decoy"}}

	in := appbuild.CargoBuildBinariesInput{Dir: dir, BinaryName: "renamed", CrateBinaryName: "selected", Version: "1.2.3", Platforms: "linux/amd64"}
	if err := appbuild.CargoBuildBinaries(t.Context(), tool, io.Discard, io.Discard, in); err != nil {
		t.Fatal(err)
	}

	if len(tool.calls) != 1 || !slices.Equal(tool.calls[0].Args, []string{"build", "--release", "--locked", "--target", "x86_64-unknown-linux-gnu", "--bin", "selected", "--target-dir", filepath.Join(dir, "target")}) {
		t.Fatalf("calls=%v", tool.calls)
	}

	body, err := os.ReadFile(filepath.Join(dir, "dist/linux-amd64/renamed-linux-amd64"))
	if err != nil || string(body) != "ELFish:x86_64-unknown-linux-gnu:selected" {
		t.Fatalf("body=%q err=%v", body, err)
	}

	before := ownedTree(t, dir)
	tool = &fakeCargoTool{metadata: `{"packages":[{"name":"package","version":"1.0.0","targets":[{"name":"selected","kind":["bin"]},{"name":"decoy","kind":["bin"]}]}]}`, binaryNames: []string{"selected", "decoy"}}

	err = appbuild.CargoReleaseBuild(t.Context(), &recordingSummarySink{}, tool, io.Discard, io.Discard, appbuild.CargoReleaseBuildInput{ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir}, BinaryName: "only-a-rename", Version: "1.0.0"})
	if !errors.Is(err, errs.ErrUsage) || len(tool.calls) != 1 || tool.calls[0].Args[0] != "metadata" || !reflect.DeepEqual(before, ownedTree(t, dir)) {
		t.Fatalf("err=%v calls=%v", err, tool.calls)
	}
}

func TestJVMPreflightBoundary_RequiredInputBeforeEffects(t *testing.T) {
	t.Parallel()

	for _, ecosystem := range []string{"maven", "gradle"} {
		t.Run(ecosystem, func(t *testing.T) {
			dir := newGradleDir(t)
			if ecosystem == "maven" {
				dir = newMavenDir(t)
			}

			before := ownedTree(t, dir)
			maven, gradle := &fakeMaven{}, &recordingGradle{}
			summary := &recordingSummarySink{}

			var (
				out bytes.Buffer
				err error
			)
			if ecosystem == "maven" {
				err = appbuild.MavenReleaseBuild(t.Context(), summary, maven, &out, &out, appbuild.MavenReleaseBuildInput{ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir, EnableBuildSBOM: true}, BuildType: "app"})
			} else {
				err = appbuild.GradleReleaseBuild(t.Context(), summary, gradle, &out, &out, appbuild.GradleReleaseBuildInput{ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir}, Tasks: " "})
			}

			if !errors.Is(err, errs.ErrUsage) || len(maven.runs) != 0 || len(gradle.calls) != 0 || summary.buf.Len() != 0 || out.Len() != 0 || !reflect.DeepEqual(before, ownedTree(t, dir)) {
				t.Fatalf("preflight effects: err=%v maven=%v gradle=%v out=%s summary=%s", err, maven.runs, gradle.calls, &out, &summary.buf)
			}
		})
	}
}
