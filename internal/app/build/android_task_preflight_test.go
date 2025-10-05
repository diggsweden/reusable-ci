// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

type androidTaskWriter struct {
	buf   bytes.Buffer
	calls int
}

func (w *androidTaskWriter) Write(body []byte) (int, error) {
	w.calls++

	return w.buf.Write(body)
}

func TestAndroidReleaseBuild_SelectedTaskPreflight(t *testing.T) { //nolint:gocognit // refusals and precedence controls share one fully valid signed fixture and effect oracle.
	for _, tc := range []struct {
		name, override, module string
		includeAAB, refuse     bool
		wantArgs               []string
	}{
		{"selected_override", "assembleRelease\x00blocked", "widget", true, true, nil},
		{"selected_derived_module", "", "widget\x00", true, true, nil},
		{"colon_override_words", " \t:widget:assembleRelease\nwidget:bundleRelease\r\n", "widget", true, false, []string{":widget:assembleRelease", "widget:bundleRelease"}},
		{"blank_override_derives", " \t\n", "widget", true, false, []string{"assembleRelease", "widget:bundleRelease"}},
		{"override_ignores_unused_fields", ":widget:assembleRelease", "widget\x00", true, false, []string{":widget:assembleRelease"}},
		{"no_bundle_ignores_module", "", "widget\x00", false, false, []string{"assembleRelease"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			wrapper := fsys.WriteFile("project/gradlew", []byte("inert wrapper; fake Gradle only\n"))
			require.NoError(t, os.Chmod(wrapper, 0o644)) //nolint:gosec // explicit non-executable owned fixture mode.
			fsys.WriteFile("project/gradle.properties", []byte("versionName=1.2.3\nversionCode=42\n"))
			fsys.WriteFile("signing/caller", []byte("signing scratch canary"))
			fsys.WriteFile("os-temp/caller", []byte("SBOM scratch canary"))
			t.Setenv("TMPDIR", fsys.Path("os-temp"))

			for _, key := range []string{"ANDROID_KEYSTORE_PATH", "ANDROID_KEYSTORE_PASSWORD", "ANDROID_KEY_ALIAS", "ANDROID_KEY_PASSWORD"} {
				t.Setenv(key, "ambient-"+key)
			}

			in := appbuild.AndroidReleaseBuildInput{ //nolint:gosec // synthetic credential canaries; no host secrets.
				ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: fsys.Path("project"), ArtifactName: "release-bundle", EnableBuildSBOM: true},
				RepoName:            "wallet", BuildTypes: "release", GradleTasksOverride: tc.override, BuildModule: tc.module, IncludeAAB: tc.includeAAB,
				EnableSigning: true, KeystoreBase64: base64.StdEncoding.EncodeToString([]byte("owned-keystore")),
				KeystorePassword: "owned-store-password", KeyAlias: "owned-alias", KeyPassword: "owned-key-password",
				SecretsPropertiesBase64: base64.StdEncoding.EncodeToString([]byte("token=owned-properties\n")),
				TempDir:                 fsys.Path("signing"), SBOMToolVersion: "2.3.4",
			}
			if tc.name == "override_ignores_unused_fields" {
				// Only the build-type selection is genuinely unused once an
				// override supplies the tasks. The flavor and the prefix still
				// compose the artifact names this build publishes, so they are
				// checked like any other value that reaches an output sink.
				in.BuildTypes = "unused-types\x00"
			}

			require.NoFileExists(t, fsys.Path("project/secrets.properties"))
			before, beforeEnv := ownedTree(t, fsys.Root), os.Environ()
			beforeSigning, beforeTemp := ownedTree(t, in.TempDir), ownedTree(t, fsys.Path("os-temp"))

			var events []string

			sink := &androidLifecycleSink{Sink: fakeoutputsink.New(t), events: &events}

			var stdout, stderr, annotations androidTaskWriter

			ops := &androidGradleRecorder{}
			ops.run = func(call androidGradleCall) error {
				require.Equal(t, in.Dir, call.Dir)
				require.Same(t, &stdout, call.Stdout)
				require.Same(t, &stderr, call.Stderr)
				require.Len(t, call.Env, 4)
				keyPath := strings.TrimPrefix(call.Env[0], "ANDROID_KEYSTORE_PATH=")
				require.Equal(t, in.TempDir, filepath.Dir(filepath.Dir(keyPath)))

				for path, want := range map[string]string{keyPath: "owned-keystore", fsys.Path("project/secrets.properties"): "token=owned-properties\n"} {
					body, readErr := os.ReadFile(path)
					require.NoError(t, readErr)
					require.Equal(t, want, string(body))

					info, statErr := os.Stat(path)
					require.NoError(t, statErr)
					require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
				}

				for _, arg := range call.Args {
					if strings.ContainsRune(arg, '\x00') {
						t.Logf("unrepresentable argv reached fake after both secret files were staged: %q", call.Args)

						return errors.New("fake refuses unrepresentable argv") //nolint:err113 // synthetic native-boundary refusal, not a preflight validation error.
					}
				}

				if len(ops.calls) == 2 {
					require.Len(t, call.Args, 3)
					require.Equal(t, []string{"--init-script", call.Args[1], "cyclonedxBom"}, call.Args)
					require.Equal(t, fsys.Path("os-temp"), filepath.Dir(call.Args[1]))
					fsys.WriteFile("project/build/reports/bom.json", []byte(`{"bomFormat":"CycloneDX","specVersion":"1.6","version":1}`))
				}

				return nil
			}

			err := appbuild.AndroidReleaseBuild(t.Context(), sink, sink, ops, output.NewAnnotator(&annotations, output.FormatGitHub), &stdout, &stderr, in)
			if tc.refuse {
				assert.True(t, errors.Is(err, errs.ErrUsage) || errors.Is(err, errs.ErrValidation), "expected task preflight classification, got %v", err)
				assert.Contains(t, fmt.Sprint(err), "tasks")
				assert.Empty(t, ops.calls, "preflight must not call Gradle")
				assert.Empty(t, events, "preflight must not call output or summary sinks")
				assert.Empty(t, sink.Keys())
				assert.Zero(t, stdout.calls+stderr.calls+annotations.calls, "preflight must not call writers")
				assert.Empty(t, stdout.buf.String()+stderr.buf.String()+annotations.buf.String()+sink.summary)
				assert.Equal(t, before, ownedTree(t, fsys.Root), "preflight must preserve paths, bytes and modes")
				assert.NotContains(t, fmt.Sprint(err), "assembleRelease")
				assert.NotContains(t, fmt.Sprint(err), "blocked")
				assert.NotContains(t, fmt.Sprint(err), "widget")
			} else {
				require.NoError(t, err)
				require.Len(t, ops.calls, 2)
				require.Equal(t, tc.wantArgs, ops.calls[0].Args)
				require.Equal(t, []string{"debug-name", "release-name", "aab-name", "sbom-name", "version", "version-code", "summary"}, events)
				require.Equal(t, "release-bundle-release", sink.Single("release-name"))
				require.Contains(t, sink.summary, fsys.Path("project/build/reports/bom.json"))
				require.Positive(t, stdout.calls)
				require.Positive(t, stderr.calls)
			}

			assert.Zero(t, sink.CloseCount())
			assert.Equal(t, beforeEnv, os.Environ())
			assert.Equal(t, beforeSigning, ownedTree(t, in.TempDir))
			assert.Equal(t, beforeTemp, ownedTree(t, fsys.Path("os-temp")))
			require.NoFileExists(t, fsys.Path("project/secrets.properties"))

			for _, secret := range []string{in.KeystorePassword, in.KeyAlias, in.KeyPassword, in.KeystoreBase64, in.SecretsPropertiesBase64, "owned-keystore", "owned-properties"} {
				assert.NotContains(t, fmt.Sprint(err)+stdout.buf.String()+stderr.buf.String()+annotations.buf.String()+sink.summary+fmt.Sprint(sink.AllScalar()), secret)
			}
		})
	}
}
