// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/toolrecorder"
	"github.com/stretchr/testify/require"
)

// TestEcosystemMetadata_RefusesLocallyDecidableInputBeforeAnyTool covers the
// metadata every release build reads from the project before it runs anything.
//
// Each ecosystem answers "what am I building and at what version" from a file
// in the working directory: go.mod, Cargo.toml, pom.xml, gradle.properties,
// package.json. Whether that answer is usable is decidable right there, without
// asking the tool. A build that starts anyway spends a full toolchain run to
// fail, and worse, a half-formed identity can reach an artifact name, a summary
// or a publish step before anyone notices. So the refusal has to come first,
// with no external call made at all.
//
// This is the locally decidable half only. Whether the tool agrees with the
// file, and whatever a native resolver would say about it, stays the tool's
// business and is not emulated here.
func TestEcosystemMetadata_RefusesLocallyDecidableInputBeforeAnyTool(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		arrange func(t *testing.T, dir string)
		run     func(t *testing.T, rec *toolrecorder.Recorder, dir string, stdout *bytes.Buffer) error
	}{
		{
			name: "go/missing module file",
			arrange: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.Remove(filepath.Join(dir, "go.mod")))
			},
			run: runGoRelease,
		},
		{
			name: "go/module file without a module path",
			arrange: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("go 1.26\n"), 0o600))
			},
			run: runGoRelease,
		},
		{
			name: "go/control character in the version",
			run: func(t *testing.T, rec *toolrecorder.Recorder, dir string, stdout *bytes.Buffer) error {
				t.Helper()

				return appbuild.GoReleaseBuild(context.Background(), &recordingSummarySink{}, rec, rec, stdout, stdout, appbuild.GoReleaseBuildInput{
					ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir},
					Version:             "1.2.3\nrelease",
					Platforms:           "linux/amd64",
				})
			},
		},
		{
			name: "go/control character in the binary name",
			run: func(t *testing.T, rec *toolrecorder.Recorder, dir string, stdout *bytes.Buffer) error {
				t.Helper()

				return appbuild.GoReleaseBuild(context.Background(), &recordingSummarySink{}, rec, rec, stdout, stdout, appbuild.GoReleaseBuildInput{
					ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir},
					BinaryName:          "app\x00name",
					Version:             "1.2.3",
					Platforms:           "linux/amd64",
				})
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dir := newGoModDir(t)
			if testCase.arrange != nil {
				testCase.arrange(t, dir)
			}

			rec := toolrecorder.New(t)

			var stdout bytes.Buffer

			err := testCase.run(t, rec, dir, &stdout)

			require.Error(t, err, "a metadata answer this build cannot use must refuse")
			require.Emptyf(t, rec.Calls, "no tool may run before the project's own metadata is usable: %q", rec.Args())
		})
	}
}

func runGoRelease(t *testing.T, rec *toolrecorder.Recorder, dir string, stdout *bytes.Buffer) error {
	t.Helper()

	return appbuild.GoReleaseBuild(context.Background(), &recordingSummarySink{}, rec, rec, stdout, stdout, appbuild.GoReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir},
		Version:             "1.2.3",
		Platforms:           "linux/amd64",
	})
}

// TestEcosystemMetadata_NPMAndGradleRefuseTheirOwnFiles covers the two
// ecosystems whose identity lives in a file with a shape of its own, so a
// malformed one is a different failure from a missing field.
func TestEcosystemMetadata_NPMAndGradleRefuseTheirOwnFiles(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		file    string
		body    string
		refused bool
	}{
		{name: "npm/malformed package.json", file: "package.json", body: "{not json", refused: true},
		{name: "npm/blank name", file: "package.json", body: `{"name":"   ","version":"1.0.0"}`, refused: true},
		{name: "npm/blank version", file: "package.json", body: `{"name":"@org/app","version":" "}`, refused: true},
		{name: "npm/control character in the name", file: "package.json", body: "{\"name\":\"@org/a\\u0000pp\",\"version\":\"1.0.0\"}", refused: true},
		{name: "npm/usable identity", file: "package.json", body: `{"name":"@org/app","version":"1.0.0","scripts":{"build":"tsc"}}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, testCase.file), []byte(testCase.body), 0o600))
			rec := toolrecorder.New(t)
			npm := toolrecorder.NPMView{Recorder: rec.Respond(npmToolResponses(rec))}

			var stdout bytes.Buffer

			err := appbuild.NPMReleaseBuild(context.Background(), &recordingSummarySink{}, npm, npm, output.Annotator{}, &stdout, &stdout, appbuild.NPMReleaseBuildInput{
				ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir},
				PackageScope:        "@org",
			})

			if !testCase.refused {
				require.NoError(t, err)
				require.NotEmpty(t, rec.Calls, "a usable identity must reach the tool")

				return
			}

			require.Error(t, err)
			require.Emptyf(t, rec.Calls, "no tool may run before the package identity is usable: %q", rec.Args())
		})
	}
}

// TestAndroidMetadata_RefusesNoncredentialInputBeforeAnyGradleCall completes the
// Android half of the same rule. Signing credentials were already checked before
// effects; the values that are not secrets were not. A product flavor, a name
// prefix or a repository name flows into the artifact names this build publishes
// as job outputs, and a resolved task becomes an argv entry handed to Gradle.
// Each of those is decidable from the inputs alone, so none of them may reach a
// name, a sink or a child process unchecked.
//
// The SBOM plugin version rides along for the same reason it does elsewhere: it
// selects what gets installed, so a value that is not an exact release is not a
// pin.
func TestAndroidMetadata_RefusesNoncredentialInputBeforeAnyGradleCall(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		mutate  func(in *appbuild.AndroidReleaseBuildInput)
		refused bool
	}{
		{name: "usable inputs reach gradle", mutate: func(*appbuild.AndroidReleaseBuildInput) {}},
		{
			name:    "control character in the product flavor",
			mutate:  func(in *appbuild.AndroidReleaseBuildInput) { in.ProductFlavor = "fdroid\x00" },
			refused: true,
		},
		{
			name:    "newline in the artifact name prefix",
			mutate:  func(in *appbuild.AndroidReleaseBuildInput) { in.ArtifactNamePrefix = "nightly\nbuild" },
			refused: true,
		},
		{
			name:    "control character in the repository name",
			mutate:  func(in *appbuild.AndroidReleaseBuildInput) { in.RepoName = "wallet\x1b" },
			refused: true,
		},
		{
			name:    "invalid UTF-8 in the artifact name override",
			mutate:  func(in *appbuild.AndroidReleaseBuildInput) { in.ArtifactName = "release\xffbundle" },
			refused: true,
		},
		{
			name:    "control character in an overridden task",
			mutate:  func(in *appbuild.AndroidReleaseBuildInput) { in.GradleTasksOverride = "assembleRelease\x00blocked" },
			refused: true,
		},
		{
			name:    "unpinned SBOM plugin version",
			mutate:  func(in *appbuild.AndroidReleaseBuildInput) { in.SBOMToolVersion = "latest" },
			refused: true,
		},
		{
			name:    "partial SBOM plugin version",
			mutate:  func(in *appbuild.AndroidReleaseBuildInput) { in.SBOMToolVersion = "2.3" },
			refused: true,
		},
		{
			name: "tabs still separate tasks",
			mutate: func(in *appbuild.AndroidReleaseBuildInput) {
				in.GradleTasksOverride = " assembleRelease\t:widget:bundleRelease "
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			wrapper := fsys.WriteFile("project/gradlew", []byte("inert wrapper; fake Gradle only\n"))
			require.NoError(t, os.Chmod(wrapper, 0o755)) //nolint:gosec // the wrapper must be executable.
			fsys.WriteFile("project/gradle.properties", []byte("versionName=1.2.3\nversionCode=42\n"))

			in := appbuild.AndroidReleaseBuildInput{
				ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: fsys.Path("project"), EnableBuildSBOM: true, SkipTests: true},
				RepoName:            "wallet",
				BuildTypes:          "release",
				SBOMToolVersion:     "2.3.4",
				TempDir:             fsys.MkdirAll("scratch"),
			}
			testCase.mutate(&in)

			rec := toolrecorder.New(t)
			sink := fakeoutputsink.New(t)

			var stdout, stderr, annotations bytes.Buffer

			err := appbuild.AndroidReleaseBuild(t.Context(), sink, &recordingSummarySink{}, rec.Android(), output.NewAnnotator(&annotations, output.FormatGitHub), &stdout, &stderr, in)

			if !testCase.refused {
				require.NoError(t, err, stdout.String())
				require.NotEmpty(t, rec.Calls, "usable inputs must reach Gradle")

				return
			}

			require.Error(t, err)
			require.Emptyf(t, rec.Calls, "no Gradle call may run before the inputs are usable: %q", rec.Args())
			require.Empty(t, sink.Keys(), "a refused build publishes no outputs")
		})
	}
}

// TestAndroidSelection_NamesEveryPublishedArtifact completes the Android
// selection matrix at the boundary that matters: the four names the workflow's
// upload steps consume as job outputs.
//
// The pure resolver has its own combination coverage, but a name that never
// reaches the sink, or reaches it under the wrong key, breaks the upload just as
// completely as a wrong name. Composition changes what every one of these is
// called, so each combination is asserted by exact value, and the override is
// asserted to displace all of it.
func TestAndroidSelection_NamesEveryPublishedArtifact(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(in *appbuild.AndroidReleaseBuildInput)
		want   map[string]string
	}{
		{
			name:   "repository name alone",
			mutate: func(*appbuild.AndroidReleaseBuildInput) {},
			want: map[string]string{
				"debug-name": "wallet - APK debug", "release-name": "wallet - APK release",
				"aab-name": "wallet - AAB release", "sbom-name": "wallet - build SBOM",
			},
		},
		{
			name:   "flavor qualifies every name",
			mutate: func(in *appbuild.AndroidReleaseBuildInput) { in.ProductFlavor = "fdroid" },
			want: map[string]string{
				"debug-name": "wallet - fdroid - APK debug", "release-name": "wallet - fdroid - APK release",
				"aab-name": "wallet - fdroid - AAB release", "sbom-name": "wallet - fdroid - build SBOM",
			},
		},
		{
			name: "prefix and flavor together",
			mutate: func(in *appbuild.AndroidReleaseBuildInput) {
				in.ArtifactNamePrefix, in.ProductFlavor = "nightly", "fdroid"
			},
			want: map[string]string{
				"debug-name": "nightly - wallet - fdroid - APK debug", "release-name": "nightly - wallet - fdroid - APK release",
				"aab-name": "nightly - wallet - fdroid - AAB release", "sbom-name": "nightly - wallet - fdroid - build SBOM",
			},
		},
		{
			name: "an override displaces the whole composition",
			mutate: func(in *appbuild.AndroidReleaseBuildInput) {
				in.ArtifactName, in.ArtifactNamePrefix, in.ProductFlavor = "release-bundle", "nightly", "fdroid"
				in.IncludeDateStamp = true
			},
			want: map[string]string{
				"debug-name": "release-bundle-debug", "release-name": "release-bundle-release",
				"aab-name": "release-bundle", "sbom-name": "release-bundle-sbom",
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			rec := toolrecorder.New(t)
			sink := fakeoutputsink.New(t)
			in := appbuild.AndroidReleaseBuildInput{
				ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newGradleDir(t), SkipTests: true},
				RepoName:            "wallet",
				BuildTypes:          "release",
				BuildModule:         "app",
			}
			testCase.mutate(&in)

			require.NoError(t, appbuild.AndroidReleaseBuild(t.Context(), sink, &recordingSummarySink{}, rec.Android(), output.Annotator{}, io.Discard, io.Discard, in))

			for key, want := range testCase.want {
				require.Equalf(t, want, sink.Single(key), "output %q", key)
			}
		})
	}
}
