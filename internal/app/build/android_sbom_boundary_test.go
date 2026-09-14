// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestAndroidReleaseBuild_SBOMTempPreflight(t *testing.T) { //nolint:gocognit // compare early refusals with enabled alias and disabled invalid-parent controls.
	for _, name := range []string{"missing", "readonly", "valid", "alias", "disabled_missing", "disabled_readonly"} {
		t.Run(name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			fsys.WriteFile("project/gradlew", []byte("inert wrapper"))
			fsys.WriteFile("signing/caller", []byte("signing canary"))
			in := appbuild.AndroidReleaseBuildInput{
				ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: fsys.Path("project"), EnableBuildSBOM: !strings.HasPrefix(name, "disabled_")},
				RepoName:            "wallet", BuildTypes: "release", SBOMToolVersion: "2.3.4",
				EnableSigning: true, KeystoreBase64: "aw==", KeystorePassword: "store", KeyAlias: "alias", KeyPassword: "key",
				TempDir: fsys.Path("signing"), SecretsPropertiesBase64: "cA==",
			}
			osParent := fsys.MkdirAll("os-temp")

			tempDir := filepath.Join(osParent, "missing")
			if !strings.HasSuffix(name, "missing") {
				tempDir = fsys.MkdirAll("os-temp/selected")
				fsys.WriteFile("os-temp/selected/caller", []byte("SBOM temp canary"))
			}

			if strings.HasSuffix(name, "readonly") {
				t.Cleanup(func() { require.NoError(t, os.Chmod(tempDir, 0o700)) }) //nolint:gosec // restore the owned temp parent before fixture cleanup.
				require.NoError(t, os.Chmod(tempDir, 0o500))                       //nolint:gosec // known nonwritable owned directory.
			}

			selected := tempDir
			if name == "alias" {
				selected = fsys.Path("os-temp/alias")
				require.NoError(t, os.Symlink(tempDir, selected))
			}

			t.Setenv("TMPDIR", selected)

			for _, key := range []string{"ANDROID_KEYSTORE_PATH", "ANDROID_KEYSTORE_PASSWORD", "ANDROID_KEY_ALIAS", "ANDROID_KEY_PASSWORD"} {
				t.Setenv(key, "ambient-"+key)
			}

			before, beforeTemp, env := ownedTree(t, fsys.Root), ownedTree(t, osParent), os.Environ()
			sink, summary, ops := fakeoutputsink.New(t), &recordingSummarySink{}, &androidGradleRecorder{}

			var stdout, stderr bytes.Buffer

			ops.run = func(call androidGradleCall) error {
				require.Equal(t, in.Dir, call.Dir)
				require.Len(t, call.Env, 4)
				keyPath := strings.TrimPrefix(call.Env[0], "ANDROID_KEYSTORE_PATH=")
				require.Equal(t, in.TempDir, filepath.Dir(filepath.Dir(keyPath)))

				if len(ops.calls) == 2 {
					require.Equal(t, []string{"--init-script", call.Args[1], "cyclonedxBom"}, call.Args)
					require.Equal(t, tempDir, filepath.Dir(call.Args[1]))
					fsys.WriteFile("project/build/reports/bom.json", []byte(`{"bomFormat":"CycloneDX","specVersion":"1.6","version":1}`))
				}

				return nil
			}
			err := appbuild.AndroidReleaseBuild(t.Context(), sink, summary, ops, output.NewAnnotator(&stderr, output.FormatGitHub), &stdout, &stderr, in)

			if name == "missing" || name == "readonly" {
				want := os.ErrNotExist
				if name == "readonly" {
					want = errs.ErrPermissionDenied
				}

				require.ErrorIs(t, err, want)
				require.Empty(t, ops.calls)
				require.Empty(t, sink.Keys())
				require.Empty(t, stdout.String()+stderr.String()+summary.buf.String())
				require.Equal(t, before, ownedTree(t, fsys.Root))
			} else {
				require.NoError(t, err)

				wantCalls := 1
				if in.EnableBuildSBOM {
					wantCalls = 2
				}

				require.Len(t, ops.calls, wantCalls)
				require.Equal(t, "wallet - APK release", sink.Single("release-name"))
				require.NoFileExists(t, fsys.Path("project/secrets.properties"))
			}

			require.Equal(t, beforeTemp, ownedTree(t, osParent))
			require.Equal(t, env, os.Environ())
		})
	}
}

func TestAndroidReleaseBuild_SBOMCleanupErrors(t *testing.T) {
	for _, nativeFailure := range []bool{false, true} {
		t.Run(fmt.Sprintf("native_failure_%t", nativeFailure), func(t *testing.T) {
			fsys := testfs.NewReal(t)
			fsys.WriteFile("project/gradlew", []byte("inert wrapper"))
			sbomTemp := fsys.MkdirAll("sbom-temp")
			t.Setenv("TMPDIR", sbomTemp)

			in := appbuild.AndroidReleaseBuildInput{
				ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: fsys.Path("project"), EnableBuildSBOM: true},
				RepoName:            "wallet", BuildTypes: "release", SBOMToolVersion: "2.3.4",
				EnableSigning: true, KeystoreBase64: "aw==", KeystorePassword: "store", KeyAlias: "alias", KeyPassword: "key",
				TempDir: fsys.MkdirAll("signing"), SecretsPropertiesBase64: "cA==",
			}
			cause := errors.New("owned native SBOM failure") //nolint:err113 // independent native-run sentinel.

			var (
				initPath string
				initBody []byte
			)

			t.Cleanup(func() {
				require.NoError(t, os.Chmod(sbomTemp, 0o700)) //nolint:gosec // restore the owned nonsecret script parent.

				if initPath != "" {
					require.NoError(t, os.Remove(initPath))
					require.NoFileExists(t, initPath)
				}
			})

			ops, summary := &androidGradleRecorder{}, &recordingSummarySink{}
			ops.run = func(call androidGradleCall) error {
				require.Equal(t, in.Dir, call.Dir)
				require.Len(t, call.Env, 4)
				keyPath := strings.TrimPrefix(call.Env[0], "ANDROID_KEYSTORE_PATH=")
				require.Equal(t, in.TempDir, filepath.Dir(filepath.Dir(keyPath)))

				if len(ops.calls) == 1 {
					return nil
				}

				require.Len(t, call.Args, 3)
				initPath = call.Args[1]
				require.Equal(t, []string{"--init-script", initPath, "cyclonedxBom"}, call.Args)
				require.Equal(t, sbomTemp, filepath.Dir(initPath))

				var err error

				initBody, err = os.ReadFile(initPath)
				require.NoError(t, err)
				require.Contains(t, string(initBody), "org.cyclonedx:cyclonedx-gradle-plugin:2.3.4")
				fsys.WriteFile("project/build/reports/bom.json", []byte(`{"bomFormat":"CycloneDX","specVersion":"1.6","version":1}`))
				require.NoError(t, os.Chmod(sbomTemp, 0o500)) //nolint:gosec // only the SBOM parent loses write permission, not the keystore parent.

				if nativeFailure {
					return cause
				}

				return nil
			}
			err := appbuild.AndroidReleaseBuild(t.Context(), fakeoutputsink.New(t), summary, ops, output.Annotator{}, io.Discard, io.Discard, in)
			require.ErrorIs(t, err, os.ErrPermission)

			if nativeFailure {
				require.ErrorIs(t, err, cause)
			}

			require.Len(t, ops.calls, 2)
			require.Contains(t, summary.buf.String(), "Generation failed")

			residue, readErr := os.ReadFile(initPath)
			require.NoError(t, readErr)
			require.Equal(t, initBody, residue)

			info, statErr := os.Stat(initPath)
			require.NoError(t, statErr)
			require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

			entries, readErr := os.ReadDir(in.TempDir)
			require.NoError(t, readErr)
			require.Empty(t, entries, "keystore cleanup is independent of SBOM script removal")
			require.NoFileExists(t, fsys.Path("project/secrets.properties"))
		})
	}
}

func TestAndroidReleaseBuild_IntegratedArtifactNames(t *testing.T) {
	for _, testCase := range []struct {
		name, override string
		includeDate    bool
		want           map[string]string
	}{
		{"composed", "", false, map[string]string{
			"debug-name": "agency - wallet - demoFree - APK debug", "release-name": "agency - wallet - demoFree - APK release",
			"aab-name": "agency - wallet - demoFree - AAB release", "sbom-name": "agency - wallet - demoFree - build SBOM",
			"version": "unknown", "version-code": "unknown",
		}},
		{"override", "release-42", true, map[string]string{
			"debug-name": "release-42-debug", "release-name": "release-42-release", "aab-name": "release-42", "sbom-name": "release-42-sbom",
			"version": "unknown", "version-code": "unknown",
		}},
		{"dated_composed", "", true, map[string]string{
			"debug-name": "2023-11-14 - agency - wallet - demoFree - APK debug", "release-name": "2023-11-14 - agency - wallet - demoFree - APK release",
			"aab-name": "2023-11-14 - agency - wallet - demoFree - AAB release", "sbom-name": "2023-11-14 - agency - wallet - demoFree - build SBOM",
			"version": "unknown", "version-code": "unknown",
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			t.Setenv("TMPDIR", fsys.MkdirAll("fallback"))
			t.Setenv("SOURCE_DATE_EPOCH", "1700000000")
			fsys.WriteFile("project/gradlew", []byte("inert wrapper"))

			epoch, parseErr := strconv.ParseInt(os.Getenv("SOURCE_DATE_EPOCH"), 10, 64)
			require.NoError(t, parseErr)
			// ArtifactNames uses time.Now, not SOURCE_DATE_EPOCH. Pin its clock
			// to the owned epoch here without changing production date policy.
			synctest.Test(t, func(t *testing.T) {
				time.Sleep(time.Until(time.Unix(epoch, 0)))

				sink, ops := fakeoutputsink.New(t), &androidGradleRecorder{}
				err := appbuild.AndroidReleaseBuild(t.Context(), sink, &recordingSummarySink{}, ops, output.Annotator{}, io.Discard, io.Discard, appbuild.AndroidReleaseBuildInput{
					ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: fsys.Path("project"), ArtifactName: testCase.override},
					ArtifactNamePrefix:  "agency", RepoName: "wallet", ProductFlavor: "demoFree", IncludeDateStamp: testCase.includeDate,
					BuildTypes: "release", IncludeAAB: true, BuildModule: "application",
				})
				require.NoError(t, err)
				require.Len(t, ops.calls, 1)
				require.Equal(t, []string{"assembleDemoFreeRelease", "application:bundleDemoFreeRelease"}, ops.calls[0].Args)
				require.Equal(t, testCase.want, sink.AllScalar())
			})
		})
	}
}
