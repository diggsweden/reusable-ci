// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

var errReportBoundary = errors.New("owned summary failure")
var errGenerationBoundary = errors.New("owned generation failure")

type projectGradleRecorder struct {
	calls                  []appbuild.GoRunInput
	initBody               string
	sbomErr                error
	checkAndroidProperties func(string)
}

func (f *projectGradleRecorder) RunInDirEnvInherit(ctx context.Context, dir string, _ []string, out, stderr io.Writer, args ...string) error {
	if f.checkAndroidProperties != nil {
		f.checkAndroidProperties(dir)
	}

	return f.RunInDirInherit(ctx, dir, out, stderr, args...)
}

func (f *projectGradleRecorder) RunInherit(context.Context, io.Writer, io.Writer, ...string) error {
	return errs.ErrUsage
}
func (f *projectGradleRecorder) RunInDirInherit(_ context.Context, dir string, out, stderr io.Writer, args ...string) error {
	f.calls = append(f.calls, appbuild.GoRunInput{Dir: dir, Args: slices.Clone(args), Stdout: out, Stderr: stderr})
	if args[0] == "--init-script" {
		body, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}

		f.initBody = string(body)
		if f.sbomErr != nil {
			return f.sbomErr
		}

		reports := filepath.Join(dir, "build/reports")
		if err := os.MkdirAll(reports, 0o700); err != nil {
			return err
		}

		return os.WriteFile(filepath.Join(reports, "bom.json"), []byte(`{"bomFormat":"CycloneDX","specVersion":"1.6","version":1}`), 0o600)
	}

	return nil
}

func TestProjectDirectoryBoundary_GradleAndAndroid(t *testing.T) { //nolint:gocognit // the two public builders share one deliberately conflicting cwd/project fixture.
	fsys := testfs.NewReal(t)
	t.Setenv("TMPDIR", fsys.MkdirAll("fallback"))
	fsys.Chdir()
	fsys.WriteFile("gradle.properties", []byte("version=cwd-canary\nversionName=cwd-canary\nversionCode=1\n"))
	fsys.WriteFile("gradlew", []byte("cwd wrapper"))
	fsys.WriteFile("build/reports/bom.json", []byte("cwd SBOM decoy"))
	dir := fsys.MkdirAll("nested project")
	fsys.WriteFile("nested project/gradlew", []byte("requested wrapper"))
	fsys.WriteFile("nested project/gradle.properties", []byte("version=2.3.4\nversionName=9.8.7\nversionCode=41\n"))

	for _, android := range []bool{false, true} {
		ops := &projectGradleRecorder{}
		summary := &recordingSummarySink{}
		sink := fakeoutputsink.New(t)

		var (
			out, stderr bytes.Buffer
			err         error
		)

		if android { //nolint:nestif // each builder has distinct metadata assertions in the same directory-boundary fixture.
			ops.checkAndroidProperties = func(calledDir string) {
				if calledDir != dir || string(fsys.ReadFile("nested project/secrets.properties")) != "fixture=owned\n" {
					t.Fatal("Android call lost exact properties bytes or selected directory")
				}
			}

			err = appbuild.AndroidReleaseBuild(t.Context(), sink, summary, ops, output.Annotator{}, &out, &stderr, appbuild.AndroidReleaseBuildInput{ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: "nested project/.", SkipTests: true, EnableBuildSBOM: true}, RepoName: "mobile", GradleTasksOverride: "assembleWidget bundleWidget", SBOMToolVersion: "2.1.0", SecretsPropertiesBase64: base64.StdEncoding.EncodeToString([]byte("fixture=owned\n"))})
			if sink.Single("version") != "9.8.7" || sink.Single("version-code") != "41" {
				t.Fatalf("Android metadata=%v", sink.AllScalar())
			}

			if _, statErr := os.Stat(fsys.Path("nested project/secrets.properties")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatal("injected properties survived Android return")
			}
		} else {
			err = appbuild.GradleReleaseBuild(t.Context(), summary, ops, &out, &stderr, appbuild.GradleReleaseBuildInput{ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: "nested project/.", SkipTests: true, EnableBuildSBOM: true}, Tasks: "assembleWidget bundleWidget", SBOMToolVersion: "2.1.0"})
			if !strings.Contains(summary.buf.String(), "2.3.4") {
				t.Fatalf("summary=%s", &summary.buf)
			}

			if metadataErr := appbuild.GradleMetadata(t.Context(), sink, &out, output.Annotator{}, appbuild.GradleMetadataInput{Dir: dir}); metadataErr != nil {
				t.Fatal(metadataErr)
			}

			if sink.Single("version") != "2.3.4" {
				t.Fatalf("metadata=%v", sink.AllScalar())
			}
		}

		if err != nil {
			t.Fatal(err)
		}

		if len(ops.calls) != 2 || !slices.Equal(ops.calls[0].Args, []string{"assembleWidget", "bundleWidget", "-x", "test"}) || !strings.Contains(ops.initBody, "2.1.0") {
			t.Fatalf("calls=%v init=%s", ops.calls, ops.initBody)
		}

		for _, call := range ops.calls {
			if call.Dir != dir || call.Stdout != &out || call.Stderr != &stderr {
				t.Fatalf("lost directory/writers: %+v", call)
			}
		}

		if !strings.Contains(summary.buf.String(), dir) || strings.Contains(out.String()+stderr.String()+summary.buf.String(), "cwd-canary") {
			t.Fatalf("project identity: dir=%s summary=%s output=%s stderr=%s", dir, &summary.buf, &out, &stderr)
		}

		if _, err := os.Stat(ops.calls[1].Args[1]); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("SBOM init script survived")
		}
	}

	if _, err := os.Stat(fsys.Path("secrets.properties")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("wrote properties into cwd")
	}
}

type interpolationMaven struct {
	fakeMaven
	expressions     []string
	failAt, emptyAt int
}

func (f *interpolationMaven) EvalExpression(_ context.Context, expr string) (string, error) {
	f.expressions = append(f.expressions, expr)
	if len(f.expressions) == f.failAt {
		return "", errGenerationBoundary
	}

	if len(f.expressions) == f.emptyAt {
		return " \n", nil
	}

	return f.answers[expr], nil
}
func TestMavenInterpolationBoundary_AllFieldsAndRefusals(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("pom.xml", []byte(`<project><version>${revision}</version><groupId>${group}</groupId><artifactId>${artifact}</artifactId></project>`))

	want := []string{"project.version", "project.groupId", "project.artifactId"}
	for position := range len(want) + 1 {
		for _, empty := range []bool{false, true} {
			ops := &interpolationMaven{fakeMaven: fakeMaven{answers: map[string]string{"project.version": "7.8.9", "project.groupId": "gov.example", "project.artifactId": "component"}}}
			if empty {
				ops.emptyAt = position
			} else {
				ops.failAt = position
			}

			sink := fakeoutputsink.New(t)

			var out bytes.Buffer

			err := appbuild.MavenMetadata(t.Context(), sink, ops, &out, appbuild.MavenMetadataInput{Dir: fsys.Root})
			if position == 0 {
				if err != nil || !slices.Equal(ops.expressions, want) || out.String() != "Project: gov.example:component:7.8.9\n" || sink.Single("version") != "7.8.9" || sink.Single("group-id") != "gov.example" || sink.Single("artifact-id") != "component" {
					t.Fatalf("err=%v expressions=%v out=%s", err, ops.expressions, &out)
				}
			} else {
				cause := errGenerationBoundary
				if empty {
					cause = errs.ErrMalformedInput
				}

				if !errors.Is(err, cause) || !strings.Contains(err.Error(), want[position-1]) || !slices.Equal(ops.expressions, want[:position]) || len(sink.Keys()) != 0 || out.Len() != 0 { //nolint:gosec // position is 1..len(want) in this branch of the bounded loop.
					t.Fatalf("err=%v expressions=%v keys=%v out=%s", err, ops.expressions, sink.Keys(), &out)
				}
			}
		}
	}
}

type failingSummary struct {
	calls    []string
	accepted []string
	failAt   int
}

func (f *failingSummary) Append(_ context.Context, body string) error {
	f.calls = append(f.calls, body)
	if len(f.calls) == f.failAt {
		return errReportBoundary
	}

	f.accepted = append(f.accepted, body)

	return nil
}

type sbomMaven struct {
	fakeMaven
	generation error
}

func (f *sbomMaven) RunInherit(ctx context.Context, out, stderr io.Writer, args ...string) error {
	if strings.Contains(strings.Join(args, " "), "cyclonedx-maven-plugin") {
		return f.generation
	}

	return f.fakeMaven.RunInherit(ctx, out, stderr, args...)
}

func TestBuildSummaryBoundary_AllSummaryPositions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		positions int
		run       func(*testing.T, ci.SummarySink, error) error
	}{
		{"go", 1, func(t *testing.T, s ci.SummarySink, _ error) error {
			t.Helper()

			return appbuild.GoReleaseBuild(t.Context(), s, &fakeGoTool{}, &fakeCycloneDXGoModTool{}, io.Discard, io.Discard, appbuild.GoReleaseBuildInput{ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newGoModDir(t)}, Version: "1.2.3"})
		}},
		{"cargo", 1, func(t *testing.T, s ci.SummarySink, generation error) error {
			t.Helper()

			return appbuild.CargoReleaseBuild(t.Context(), s, &fakeCargoTool{metadata: sampleCargoMetadata, binaryNames: []string{"hello"}, failOn: "cyclonedx", runErr: generation}, io.Discard, io.Discard, appbuild.CargoReleaseBuildInput{ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: t.TempDir(), EnableBuildSBOM: true}, Version: "1.2.3"})
		}},
		{"maven", 2, func(t *testing.T, s ci.SummarySink, generation error) error {
			t.Helper()

			return appbuild.MavenReleaseBuild(t.Context(), s, &sbomMaven{generation: generation}, io.Discard, io.Discard, appbuild.MavenReleaseBuildInput{ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newMavenDir(t), EnableBuildSBOM: true}, BuildType: "app", SBOMToolVersion: "2.9.1"})
		}},
		{"gradle", 2, func(t *testing.T, s ci.SummarySink, generation error) error {
			t.Helper()

			return appbuild.GradleReleaseBuild(t.Context(), s, &projectGradleRecorder{sbomErr: generation}, io.Discard, io.Discard, appbuild.GradleReleaseBuildInput{ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newGradleDir(t), EnableBuildSBOM: true}, Tasks: "assemble", SBOMToolVersion: "2.1.0"})
		}},
		{"android", 1, func(t *testing.T, s ci.SummarySink, generation error) error {
			t.Helper()

			return appbuild.AndroidReleaseBuild(t.Context(), fakeoutputsink.New(t), s, &projectGradleRecorder{sbomErr: generation}, output.Annotator{}, io.Discard, io.Discard, appbuild.AndroidReleaseBuildInput{ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newGradleDir(t), EnableBuildSBOM: true}, RepoName: "mobile", BuildTypes: "release", BuildModule: "app", SBOMToolVersion: "2.1.0"})
		}},
		{"npm", 2, func(t *testing.T, s ci.SummarySink, generation error) error {
			t.Helper()

			return appbuild.NPMReleaseBuild(t.Context(), s, &fakeNPMRunner{}, &fakeNPMRunner{failOn: map[string]error{"--yes": generation}}, output.Annotator{}, io.Discard, io.Discard, appbuild.NPMReleaseBuildInput{ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newNPMDir(t), EnableBuildSBOM: true}, SBOMToolVersion: "4.2.1"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for position := 0; position <= tc.positions; position++ {
				summary := &failingSummary{failAt: position}

				err := tc.run(t, summary, nil)
				if position == 0 {
					if err != nil || len(summary.accepted) != tc.positions {
						t.Fatalf("positive err=%v accepted=%d", err, len(summary.accepted))
					}
				} else if !errors.Is(err, errReportBoundary) || len(summary.calls) != position || len(summary.accepted) != position-1 {
					t.Fatalf("position=%d err=%v calls=%d accepted=%d", position, err, len(summary.calls), len(summary.accepted))
				}
			}

			if tc.name != "go" {
				summary := &failingSummary{failAt: 1}

				err := tc.run(t, summary, errGenerationBoundary)
				if !errors.Is(err, errGenerationBoundary) || !errors.Is(err, errReportBoundary) || len(summary.calls) != 1 || len(summary.accepted) != 0 {
					t.Fatalf("combined failure lost: %v", err)
				}

				if strings.Index(err.Error(), errGenerationBoundary.Error()) > strings.Index(err.Error(), errReportBoundary.Error()) {
					t.Fatalf("generation must remain primary: %v", err)
				}
			}
		})
	}
}

func TestXcodeDependencyBoundary_ArchiveAndExport(t *testing.T) { //nolint:gocognit // independently exercise start errors and nonzero statuses for both Xcode operations.
	for _, export := range []bool{false, true} {
		for _, start := range []bool{false, true} {
			fsys := testfs.NewReal(t)
			fsys.Chdir()
			fsys.MkdirAll("Fixture.xcodeproj")

			ops := &fakeXcodeBuild{exitCode: 17}
			if start {
				ops.err = errors.Join(errGenerationBoundary, errs.ErrDependencyUnavailable)
			}

			var (
				out   bytes.Buffer
				err   error
				plist string
			)

			ops.run = func(args []string) {
				if export {
					plist = args[6]
				}
			}
			if export {
				err = appbuild.XcodeExportIPA(t.Context(), ops, &out, &out, appbuild.XcodeExportIPAInput{ExportOptionsBase64: "cGxpc3Q="})
			} else {
				sink := fakeoutputsink.New(t)

				err = appbuild.XcodeReleaseBuild(t.Context(), sink, &fakeSecurity{}, ops, output.Annotator{}, &out, &out, appbuild.XcodeReleaseBuildInput{RepositoryName: "fixture", Project: "Fixture.xcodeproj", Scheme: "Fixture", Configuration: "Release", Destination: "generic/platform=iOS"})
				if len(sink.Keys()) != 0 {
					t.Fatal("archive failure published outputs")
				}
			}

			if !errors.Is(err, errs.ErrDependencyUnavailable) || len(ops.calls) != 1 || strings.Contains(out.String(), "Built artifacts:") {
				t.Fatalf("err=%v calls=%v out=%s", err, ops.calls, &out)
			}

			if start && !errors.Is(err, errGenerationBoundary) {
				t.Fatalf("start cause lost: %v", err)
			}

			if plist != "" {
				if _, err := os.Stat(plist); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("export plist survived failure")
				}
			}
		}
	}
}
