// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/ghaenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

type androidCommandGradle struct {
	run   func(context.Context, string, []string, io.Writer, io.Writer, []string) error
	calls int
}

func (g *androidCommandGradle) RunInDirEnvInherit(ctx context.Context, dir string, env []string, stdout, stderr io.Writer, args ...string) error {
	g.calls++

	return g.run(ctx, dir, env, stdout, stderr, args)
}

func TestGradleAndroidRun_BindsSigningEnvironmentAndBlobSources(t *testing.T) { //nolint:gocognit,gocyclo,maintidx // one CLI-to-app oracle contrasts sources, flag precedence and both native failure positions.
	for _, source := range []string{"env", "dated_env", "file", "stdin"} {
		for _, fail := range []string{"", "build", "sbom"} {
			t.Run(source+"/fail_"+fail, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					time.Sleep(time.Until(time.Unix(1700000000, 0)))

					outputs := ghaenv.Setup(t)
					fsys := testfs.NewReal(t)
					fsys.Chdir()
					t.Setenv("TMPDIR", fsys.MkdirAll("fallback"))
					fsys.WriteFile("selected/gradlew", []byte("inert wrapper"))
					fsys.WriteFile("selected/gradle.properties", []byte("versionName=4.5.6\nversionCode=81\n"))
					fsys.WriteFile("gradlew", []byte("unselected wrapper"))
					fsys.WriteFile("gradle.properties", []byte("versionName=wrong-cwd\nversionCode=999\n"))
					fsys.WriteFile("decoy/gradlew", []byte("env decoy wrapper"))
					fsys.WriteFile("decoy/gradle.properties", []byte("versionName=wrong-env\nversionCode=999\n"))
					fsys.WriteFile("decoy/build/reports/bom.json", []byte("unselected report"))

					for key, value := range map[string]string{
						"WORKING_DIRECTORY": "selected", "INCLUDE_DATE_STAMP": "false", "ARTIFACT_NAME_PREFIX": "agency",
						"REPOSITORY_NAME": "mobile", "PRODUCT_FLAVOR": "demoFree", "ARTIFACT_NAME": "", "ENABLE_SIGNING": "true",
						"GRADLE_TASKS_OVERRIDE": "", "BUILD_TYPES": "release,debug", "INCLUDE_AAB": "true", "BUILD_MODULE": "widget",
						"SKIP_TESTS": "true", "ENABLE_BUILD_SBOM": "true", "CYCLONEDX_GRADLE_VERSION": "3.2.1",
						"CI_TEMP_DIR": fsys.MkdirAll("scratch"), "RUNNER_TEMP": fsys.MkdirAll("decoy-scratch"),
					} {
						t.Setenv(key, value)
					}

					if source == "dated_env" {
						t.Setenv("INCLUDE_DATE_STAMP", "true")
					}

					t.Setenv("ANDROID_KEYSTORE_PASSWORD", " cli-store \r\n")
					t.Setenv("ANDROID_KEY_ALIAS", " cli-alias ")
					t.Setenv("ANDROID_KEY_PASSWORD", "\tcli-key\n")
					t.Setenv("ANDROID_KEYSTORE_PATH", "ambient-path")

					keystore := base64.StdEncoding.EncodeToString([]byte("CLI keystore"))
					properties := base64.StdEncoding.EncodeToString([]byte("CLI properties\n"))

					t.Setenv("ANDROID_KEYSTORE_BASE64", keystore)
					t.Setenv("SECRETS_PROPERTIES_BASE64", properties)

					args := []string{"run"}
					wantArgs := []string{"assembleDemoFreeDebug", "assembleDemoFreeRelease", "widget:bundleDemoFreeRelease", "-x", "test"}

					if source == "file" || source == "stdin" {
						for key, value := range map[string]string{
							"WORKING_DIRECTORY": "decoy", "INCLUDE_DATE_STAMP": "true", "ARTIFACT_NAME_PREFIX": "env-prefix",
							"REPOSITORY_NAME": "env-repo", "PRODUCT_FLAVOR": "envFlavor", "ARTIFACT_NAME": "env-override", "ENABLE_SIGNING": "false",
							"GRADLE_TASKS_OVERRIDE": "envTask", "BUILD_TYPES": "release", "INCLUDE_AAB": "false", "BUILD_MODULE": "envModule",
							"SKIP_TESTS": "false", "ENABLE_BUILD_SBOM": "false", "CYCLONEDX_GRADLE_VERSION": "2.9.9", "CI_TEMP_DIR": fsys.Path("decoy-scratch"),
						} {
							t.Setenv(key, value)
						}

						args = append(args, "--working-dir", "selected/.", "--temp-dir", fsys.Path("scratch"), "--enable-signing", "--build-sbom", "--sbom-tool-version", "3.2.1",
							"--include-date=false", "--repository-name", "mobile", "--prefix", "agency", "--flavor", "demoFree", "--build-module", "widget", "--skip-tests")
						if source == "file" {
							args = append(args, "--name-override", "", "--tasks-override", "", "--build-types", "debug,release", "--include-aab")
						} else {
							args = append(args, "--name-override", "cli-release", "--tasks-override", " assembleChosen\t:widget:bundleChosen ", "--build-types", "ignored-invalid", "--include-aab=false")
							wantArgs = []string{"assembleChosen", ":widget:bundleChosen", "-x", "test"}
						}

						fsys.WriteFile("keystore.base64", []byte(os.Getenv("ANDROID_KEYSTORE_BASE64")))
						fsys.WriteFile("properties.base64", []byte(os.Getenv("SECRETS_PROPERTIES_BASE64")))
						t.Setenv("ANDROID_KEYSTORE_BASE64", "%%%")
						t.Setenv("SECRETS_PROPERTIES_BASE64", "%%%")

						keyFile := fsys.Path("keystore.base64")
						if source == "stdin" {
							file, err := os.Open(keyFile)
							require.NoError(t, err)

							oldStdin := os.Stdin
							os.Stdin = file

							t.Cleanup(func() { os.Stdin = oldStdin; require.NoError(t, file.Close()) })

							keyFile = "-"
						}

						args = append(args, "--keystore-base64-file", keyFile, "--secrets-base64-file", fsys.Path("properties.base64"))
					}

					var captured [2]*os.File

					for index, name := range []string{"stdout", "stderr"} {
						file, openErr := os.OpenFile(fsys.WriteFile(name, nil), os.O_RDWR, 0o600)
						require.NoError(t, openErr)

						captured[index] = file
					}

					oldOut, oldErr := os.Stdout, os.Stderr
					os.Stdout, os.Stderr = captured[0], captured[1]

					t.Cleanup(func() {
						os.Stdout, os.Stderr = oldOut, oldErr

						for _, file := range captured {
							require.NoError(t, file.Close())
						}
					})
					require.NoError(t, os.WriteFile(outputs.OutputPath, []byte("unrelated=caller output\n"), 0o600))
					require.NoError(t, os.WriteFile(outputs.SummaryPath, []byte("caller summary\n"), 0o600))

					before := os.Environ()

					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()

					buildCause, sbomCause := errors.New("owned CLI build failure"), errors.New("owned CLI SBOM failure") //nolint:err113 // distinct native error identities must survive the CLI.

					var (
						keyPath, initPath string
						events            []string
					)

					ops := &androidCommandGradle{run: func(callCtx context.Context, dir string, env []string, stdout, stderr io.Writer, args []string) error {
						require.Equal(t, ctx.Done(), callCtx.Done())
						require.Equal(t, fsys.Path("selected"), dir)
						require.Same(t, os.Stderr, stdout)
						require.Same(t, os.Stderr, stderr)
						require.Len(t, env, 4)

						path := strings.TrimPrefix(env[0], "ANDROID_KEYSTORE_PATH=")
						if keyPath == "" {
							keyPath = path
						}

						require.Equal(t, keyPath, path)
						require.Equal(t, fsys.Path("scratch"), filepath.Dir(filepath.Dir(keyPath)))
						require.Equal(t, []string{"ANDROID_KEYSTORE_PASSWORD= cli-store \r\n", "ANDROID_KEY_ALIAS= cli-alias ", "ANDROID_KEY_PASSWORD=\tcli-key\n"}, env[1:])

						body, err := os.ReadFile(keyPath)
						require.NoError(t, err)
						require.Equal(t, "CLI keystore", string(body))
						body, err = os.ReadFile(fsys.Path("selected/secrets.properties"))
						require.NoError(t, err)
						require.Equal(t, "CLI properties\n", string(body))

						for _, path := range []string{keyPath, fsys.Path("selected/secrets.properties")} {
							info, statErr := os.Stat(path)
							require.NoError(t, statErr)
							require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
						}

						require.Equal(t, before, os.Environ())

						step := "build"

						if len(events) == 0 {
							require.Equal(t, wantArgs, args)
						} else {
							step = "sbom"

							require.Len(t, args, 3)
							initPath = args[1]
							require.Equal(t, []string{"--init-script", initPath, "cyclonedxBom"}, args)
							require.Equal(t, fsys.Path("fallback"), filepath.Dir(initPath))
							body, readErr := os.ReadFile(initPath)
							require.NoError(t, readErr)
							require.Contains(t, string(body), "org.cyclonedx:cyclonedx-gradle-plugin:3.2.1")
							require.Contains(t, string(body), "apply<CyclonedxPlugin>()")
							fsys.WriteFile("selected/build/reports/cyclonedx/bom.json", []byte(`{"bomFormat":"CycloneDX","specVersion":"1.6","version":1}`))
						}

						events = append(events, step)
						_, err = io.WriteString(stdout, step+" owned stdout\n")
						require.NoError(t, err)
						_, err = io.WriteString(stderr, step+" owned stderr\n")
						require.NoError(t, err)

						if fail == step {
							if step == "build" {
								return buildCause
							}

							return sbomCause
						}

						return nil
					}}
					err := gradleAndroidRunCmd(ops).Run(ctx, args)
					wantSummary := "### Build SBOM\n- \u2713 CycloneDX: `" + fsys.Path("selected/build/reports/cyclonedx/bom.json") + "`\n"
					wantEvents := []string{"build", "sbom"}

					switch fail {
					case "":
						require.NoError(t, err)
					case "build":
						require.ErrorIs(t, err, buildCause)
						require.NotErrorIs(t, err, sbomCause)
						require.Equal(t, "gradle build: owned CLI build failure", err.Error())

						wantEvents, wantSummary = []string{"build"}, ""
					case "sbom":
						require.ErrorIs(t, err, sbomCause)
						require.NotErrorIs(t, err, buildCause)
						require.Equal(t, "gradle-android Build SBOM generation failed: owned CLI SBOM failure", err.Error())

						wantSummary = "### Build SBOM\n- \u26a0\ufe0f Generation failed; release blocked until the Build SBOM succeeds or is explicitly disabled\n"
					}

					require.Equal(t, wantEvents, events)
					require.Equal(t, len(wantEvents), ops.calls)

					wantNames := []string{"agency - mobile - demoFree - APK debug", "agency - mobile - demoFree - APK release", "agency - mobile - demoFree - AAB release", "agency - mobile - demoFree - build SBOM"}

					switch source {
					case "stdin":
						wantNames = []string{"cli-release-debug", "cli-release-release", "cli-release", "cli-release-sbom"}
					case "dated_env":
						for index := range wantNames {
							wantNames[index] = "2023-11-14 - " + wantNames[index]
						}
					}

					wantOutput := fmt.Sprintf("unrelated=caller output\ndebug-name=%s\nrelease-name=%s\naab-name=%s\nsbom-name=%s\nversion=4.5.6\nversion-code=81\n", wantNames[0], wantNames[1], wantNames[2], wantNames[3])
					outputBody, readErr := os.ReadFile(outputs.OutputPath)
					require.NoError(t, readErr)
					require.Equal(t, wantOutput, string(outputBody))
					require.Equal(t, "caller summary\n"+wantSummary, outputs.Summary())
					require.Empty(t, fsys.ReadFile("stdout"))

					for _, step := range wantEvents {
						require.Contains(t, string(fsys.ReadFile("stderr")), step+" owned stdout\n"+step+" owned stderr\n")
					}

					require.Equal(t, before, os.Environ())
					require.NoDirExists(t, filepath.Dir(keyPath))
					require.NoFileExists(t, fsys.Path("selected/secrets.properties"))

					if initPath != "" {
						require.NoFileExists(t, initPath)
					}

					for _, dir := range []string{"scratch", "decoy-scratch", "fallback"} {
						entries, readErr := os.ReadDir(fsys.Path(dir))
						require.NoError(t, readErr)
						require.Empty(t, entries)
					}

					for path, want := range map[string]string{"gradlew": "unselected wrapper", "gradle.properties": "versionName=wrong-cwd\nversionCode=999\n", "decoy/gradlew": "env decoy wrapper", "decoy/gradle.properties": "versionName=wrong-env\nversionCode=999\n", "decoy/build/reports/bom.json": "unselected report", "selected/gradlew": "inert wrapper"} {
						require.Equal(t, want, string(fsys.ReadFile(path)))
						info, statErr := os.Stat(fsys.Path(path))
						require.NoError(t, statErr)

						wantMode := os.FileMode(0o644)
						if path == "selected/gradlew" {
							wantMode = 0o755
						}

						require.Equal(t, wantMode, info.Mode().Perm())
					}

					for _, secret := range []string{keystore, properties, "CLI keystore", "CLI properties", "cli-store", "cli-alias", "cli-key"} {
						require.NotContains(t, string(outputBody)+outputs.Summary()+string(fsys.ReadFile("stdout"))+string(fsys.ReadFile("stderr"))+fmt.Sprint(err), secret)
					}

					if source == "file" || source == "stdin" {
						require.Equal(t, keystore, string(fsys.ReadFile("keystore.base64")))
						require.Equal(t, properties, string(fsys.ReadFile("properties.base64")))
					}
				})
			})
		}
	}
}

func TestGradleAndroidRun_MissingCredentialRefusesBeforeGradle(t *testing.T) {
	for _, missing := range []string{"ANDROID_KEYSTORE_PASSWORD", "ANDROID_KEY_ALIAS", "ANDROID_KEY_PASSWORD"} {
		t.Run(missing, func(t *testing.T) {
			outputs := ghaenv.Setup(t)
			fsys := testfs.NewReal(t)
			fsys.Chdir()
			t.Setenv("TMPDIR", fsys.MkdirAll("fallback"))
			fsys.WriteFile("gradlew", []byte("inert wrapper"))

			for _, name := range []string{"ANDROID_KEYSTORE_PASSWORD", "ANDROID_KEY_ALIAS", "ANDROID_KEY_PASSWORD"} {
				t.Setenv(name, "synthetic-"+name)
			}

			t.Setenv(missing, "")
			t.Setenv("ANDROID_KEYSTORE_BASE64", "aw==")
			t.Setenv("SECRETS_PROPERTIES_BASE64", "")

			ops := &androidCommandGradle{}
			err := gradleAndroidRunCmd(ops).Run(t.Context(), []string{"run", "--enable-signing", "--build-sbom=false"})
			require.ErrorIs(t, err, errs.ErrPermissionDenied)
			require.Contains(t, err.Error(), missing)
			require.NotContains(t, err.Error(), "synthetic-")
			require.Zero(t, ops.calls)
			require.Empty(t, outputs.Output("release-name"))

			info, err := os.Stat(fsys.Path("gradlew"))
			require.NoError(t, err)
			require.Zero(t, info.Mode().Perm()&0o111)
		})
	}
}

func TestGradleAndroidRun_DisabledOptionsSources(t *testing.T) {
	for _, source := range []string{"flags", "env_debug", "env_release"} {
		t.Run(source, func(t *testing.T) {
			outputs := ghaenv.Setup(t)
			fsys := testfs.NewReal(t)
			fsys.Chdir()
			t.Setenv("TMPDIR", fsys.MkdirAll("scratch"))
			fsys.WriteFile("gradlew", []byte("inert wrapper"))

			for key, value := range map[string]string{
				"WORKING_DIRECTORY": ".", "INCLUDE_DATE_STAMP": "true", "ARTIFACT_NAME": "env-name", "ARTIFACT_NAME_PREFIX": "", "REPOSITORY_NAME": "", "PRODUCT_FLAVOR": "",
				"ENABLE_SIGNING": "true", "ANDROID_KEYSTORE_BASE64": "%%%unused-keystore", "ANDROID_KEYSTORE_PASSWORD": "unused-store", "ANDROID_KEY_ALIAS": "unused-alias", "ANDROID_KEY_PASSWORD": "unused-key",
				"SECRETS_PROPERTIES_BASE64": "", "GRADLE_TASKS_OVERRIDE": "", "BUILD_TYPES": "debug", "BUILD_MODULE": "", "INCLUDE_AAB": "true", "SKIP_TESTS": "true", "ENABLE_BUILD_SBOM": "true", "CYCLONEDX_GRADLE_VERSION": "",
			} {
				t.Setenv(key, value)
			}

			args := []string{"run", "--enable-signing=false", "--build-sbom=false", "--skip-tests=false", "--include-aab=false", "--include-date=false", "--build-types", "release", "--name-override", "unsigned"}
			wantArgs := []string{"assembleRelease"}

			if source != "flags" {
				for key, value := range map[string]string{
					"ENABLE_SIGNING": "false", "ENABLE_BUILD_SBOM": "false", "SKIP_TESTS": "false", "INCLUDE_AAB": "false", "INCLUDE_DATE_STAMP": "false", "ARTIFACT_NAME": "unsigned", "CYCLONEDX_GRADLE_VERSION": "3.2.1",
				} {
					t.Setenv(key, value)
				}

				args = []string{"run"}

				if source == "env_debug" {
					wantArgs = []string{"assembleDebug"}
				} else {
					t.Setenv("BUILD_TYPES", "release")
				}
			}

			log, err := os.OpenFile(fsys.WriteFile("stderr", nil), os.O_RDWR, 0o600)
			require.NoError(t, err)

			oldStderr := os.Stderr
			os.Stderr = log

			t.Cleanup(func() { os.Stderr = oldStderr; require.NoError(t, log.Close()) })

			before := os.Environ()
			ops := &androidCommandGradle{run: func(_ context.Context, dir string, env []string, stdout, stderr io.Writer, args []string) error {
				require.Equal(t, fsys.Root, dir)
				require.Empty(t, env)
				require.Equal(t, wantArgs, args)
				require.Same(t, log, stdout)
				require.Same(t, log, stderr)
				require.NoFileExists(t, fsys.Path("secrets.properties"))

				return nil
			}}
			require.NoError(t, gradleAndroidRunCmd(ops).Run(t.Context(), args))
			require.Equal(t, 1, ops.calls)

			outputBody, err := os.ReadFile(outputs.OutputPath)
			require.NoError(t, err)
			require.Equal(t, "debug-name=unsigned-debug\nrelease-name=unsigned-release\naab-name=unsigned\nsbom-name=unsigned-sbom\nversion=unknown\nversion-code=unknown\n", string(outputBody))
			require.Equal(t, "### Build SBOM\n- \u2298 Generation disabled; release continues without a build SBOM\n", outputs.Summary())
			require.Contains(t, string(fsys.ReadFile("stderr")), "gradle.properties not found, version info unavailable")

			for _, secret := range []string{"unused-keystore", "unused-store", "unused-alias", "unused-key"} {
				require.NotContains(t, string(outputBody)+outputs.Summary()+string(fsys.ReadFile("stderr")), secret)
			}

			entries, err := os.ReadDir(fsys.Path("scratch"))
			require.NoError(t, err)
			require.Empty(t, entries)
			require.NoFileExists(t, fsys.Path("secrets.properties"))
			require.Equal(t, before, os.Environ())
		})
	}
}

func TestGradleAndroidRun_HelpExplainsDirectoryAndSecretLifetime(t *testing.T) {
	var help bytes.Buffer

	cmd := gradleAndroidRunCmd(&androidCommandGradle{})
	cmd.Writer = &help
	require.NoError(t, cmd.Run(t.Context(), []string{"run", "--help"}))

	for _, text := range []string{"Both ./gradlew invocations and metadata use --working-dir", "create-only", "cleaned after build and SBOM", "ANDROID_KEYSTORE_PASSWORD", "ANDROID_KEY_ALIAS", "ANDROID_KEY_PASSWORD", "whitespace-only aliases", "not a build sandbox"} {
		require.Contains(t, help.String(), text)
	}

	require.NotContains(t, help.String(), "not --working-dir")
}
