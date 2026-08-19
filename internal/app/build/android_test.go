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
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ghaoutput"
	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// TestAndroidArtifactNames_EmitsFourNames covers what this layer owns:
// which sink keys the four names land under, and that every input reaches
// the resolver. How the names themselves compose is the domain's claim,
// tested in internal/domain/build.
//
// Each case compares the whole output map rather than the keys it expects,
// so an added or renamed output is caught too -- these are consumed by the
// workflow to name uploaded artifacts.
func TestAndroidArtifactNames_EmitsFourNames(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   appbuild.AndroidArtifactNamesInput
		want map[string]string
	}{
		{
			name: "repo name only",
			in:   appbuild.AndroidArtifactNamesInput{RepoName: "demo-app"},
			want: map[string]string{
				"debug-name":   "demo-app - APK debug",
				"release-name": "demo-app - APK release",
				"aab-name":     "demo-app - AAB release",
				"sbom-name":    "demo-app - build SBOM",
			},
		},
		{
			name: "date, prefix and flavor all reach the resolver",
			in: appbuild.AndroidArtifactNamesInput{
				IncludeDate: true,
				Prefix:      "Nightly",
				RepoName:    "demo-app",
				Flavor:      "fdroid",
				Today:       time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
			},
			want: map[string]string{
				"debug-name":   "2026-05-10 - Nightly - demo-app - fdroid - APK debug",
				"release-name": "2026-05-10 - Nightly - demo-app - fdroid - APK release",
				"aab-name":     "2026-05-10 - Nightly - demo-app - fdroid - AAB release",
				"sbom-name":    "2026-05-10 - Nightly - demo-app - fdroid - build SBOM",
			},
		},
		{
			// An override replaces the composed name entirely, so a
			// consumer pinning an exact artifact name is not surprised by
			// a date or flavor appearing in it.
			name: "an override outranks every other input",
			in: appbuild.AndroidArtifactNamesInput{
				IncludeDate: true,
				Prefix:      "ci",
				RepoName:    "myapp",
				Flavor:      "prod",
				Override:    "wallet-android-demo",
				Today:       time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
			},
			want: map[string]string{
				"debug-name":   "wallet-android-demo-debug",
				"release-name": "wallet-android-demo-release",
				"aab-name":     "wallet-android-demo",
				"sbom-name":    "wallet-android-demo-sbom",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sink := fakeoutputsink.New(t)

			if err := appbuild.AndroidArtifactNames(context.Background(), sink, io.Discard, tc.in); err != nil {
				t.Fatalf("AndroidArtifactNames: %v", err)
			}

			if got := sink.AllScalar(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("outputs =\n%v\nwant\n%v", got, tc.want)
			}
		})
	}
}

func TestAndroidArtifactNames_RequiresRepoNameOrOverride(t *testing.T) {
	t.Parallel()

	sink := fakeoutputsink.New(t)

	err := appbuild.AndroidArtifactNames(context.Background(), sink, io.Discard, appbuild.AndroidArtifactNamesInput{})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	// Nothing half-emitted: a workflow reading three of four names would
	// upload artifacts under names the fourth step never agreed to.
	if got := sink.Keys(); len(got) != 0 {
		t.Errorf("emitted %q with neither repo-name nor override", got)
	}
}

func TestAndroidVersionInfo_ReadsGradleProperties(t *testing.T) {
	t.Parallel()

	fsys := testfs.NewReal(t)
	fsys.WriteFile("gradle.properties", []byte("versionName=1.0.0-beta1\nversionCode=5\n"))

	sink := fakeoutputsink.New(t)

	var stderr bytes.Buffer

	if err := appbuild.AndroidVersionInfo(context.Background(), sink, &stderr, output.Annotator{}, appbuild.AndroidVersionInfoInput{Dir: fsys.Root}); err != nil {
		t.Fatalf("AndroidVersionInfo: %v", err)
	}

	want := map[string]string{"version": "1.0.0-beta1", "version-code": "5"}
	if got := sink.AllScalar(); !reflect.DeepEqual(got, want) {
		t.Errorf("outputs = %v, want %v", got, want)
	}

	if got := strings.TrimSpace(stderr.String()); got != "Version: 1.0.0-beta1 (5)" {
		t.Errorf("stderr = %q", got)
	}
}

func TestAndroidVersionInfo_MissingFileFallsBackToUnknown(t *testing.T) {
	t.Parallel()

	fsys := testfs.NewReal(t) // no gradle.properties
	sink := fakeoutputsink.New(t)

	var stderr bytes.Buffer

	if err := appbuild.AndroidVersionInfo(context.Background(), sink, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appbuild.AndroidVersionInfoInput{Dir: fsys.Root}); err != nil {
		t.Fatalf("AndroidVersionInfo: %v", err)
	}

	// Both outputs are still emitted, so a consumer always has something
	// to read -- absence is a warning here, not a failure.
	want := map[string]string{"version": "unknown", "version-code": "unknown"}
	if got := sink.AllScalar(); !reflect.DeepEqual(got, want) {
		t.Errorf("outputs = %v, want %v", got, want)
	}

	if !strings.Contains(stderr.String(), "gradle.properties not found") {
		t.Errorf("expected warning in stderr: %q", stderr.String())
	}
}

// TestAndroidVersionInfo_CRLFPropertiesFailOnARealSink pins a defect the
// fake sink cannot see. ParseGradleVersionFromProperties splits on "\n"
// only, so a CRLF gradle.properties leaves a carriage return in both
// values, and every line-oriented sink refuses a scalar containing one.
//
// The result is that `android version-info` fails outright on a
// CRLF-checked-out Android project, with a message about newlines and
// SetMultiline that points nowhere near the cause. See
// docs/open-questions.md.
func TestAndroidVersionInfo_CRLFPropertiesFailOnARealSink(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		wantErr error
		wantOut string
	}{
		{
			name:    "LF",
			body:    "versionName=1.2.3\nversionCode=42\n",
			wantOut: "version=1.2.3\nversion-code=42\n",
		},
		{
			name:    "CRLF",
			body:    "versionName=1.2.3\r\nversionCode=42\r\n",
			wantErr: errs.ErrValidation,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			fsys.WriteFile("gradle.properties", []byte(tc.body))

			outPath := filepath.Join(t.TempDir(), "gha_output")
			if err := os.WriteFile(outPath, nil, 0o600); err != nil {
				t.Fatal(err)
			}

			t.Setenv("GITHUB_OUTPUT", outPath)

			err := appbuild.AndroidVersionInfo(context.Background(), ghaoutput.NewFromEnv(), io.Discard, output.Annotator{}, appbuild.AndroidVersionInfoInput{Dir: fsys.Root})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}

			body, readErr := os.ReadFile(outPath)
			if readErr != nil {
				t.Fatal(readErr)
			}

			if string(body) != tc.wantOut {
				t.Errorf("output = %q, want %q", body, tc.wantOut)
			}
		})
	}
}

func TestAndroidDecodeKeystore_WritesFileAndPrintsEnvLine(t *testing.T) {
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	body := []byte("\x01\x02fake-keystore-bytes\x03")
	enc := base64.StdEncoding.EncodeToString(body)

	var out bytes.Buffer

	err := appbuild.AndroidDecodeKeystore(&out, io.Discard, appbuild.AndroidDecodeKeystoreInput{
		Base64: enc, Dir: dir,
	})
	if err != nil {
		t.Fatalf("AndroidDecodeKeystore: %v", err)
	}

	want := filepath.Join(dir, "release.keystore")
	if got := strings.TrimSpace(out.String()); got != "ANDROID_KEYSTORE_PATH="+want {
		t.Errorf("out = %q, want %q", got, "ANDROID_KEYSTORE_PATH="+want)
	}

	got := fsys.ReadFile("release.keystore")
	if !bytes.Equal(got, body) {
		t.Errorf("keystore body = %q, want %q", got, body)
	}

	info, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != 0o600 {
		t.Errorf("keystore mode = %o, want 0o600", info.Mode().Perm())
	}
}

func TestAndroidDecodeKeystore_PrintsSuccessToStderr(t *testing.T) {
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	enc := base64.StdEncoding.EncodeToString([]byte("fake-keystore-content-for-testing"))

	var stderr bytes.Buffer
	if err := appbuild.AndroidDecodeKeystore(io.Discard, &stderr, appbuild.AndroidDecodeKeystoreInput{
		Base64: enc,
		Dir:    dir,
	}); err != nil {
		t.Fatalf("AndroidDecodeKeystore: %v", err)
	}

	if got := strings.TrimSpace(stderr.String()); got != "✓ Android keystore decoded successfully" {
		t.Errorf("stderr = %q", got)
	}
}

func TestAndroidDecodeKeystore_RelativeDirPrintsAbsolutePath(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.MkdirAll("project")
	t.Chdir(fsys.Root)

	enc := base64.StdEncoding.EncodeToString([]byte("fake-keystore-content-for-testing"))

	var out bytes.Buffer
	if err := appbuild.AndroidDecodeKeystore(&out, io.Discard, appbuild.AndroidDecodeKeystoreInput{
		Base64: enc,
		Dir:    "project",
	}); err != nil {
		t.Fatalf("AndroidDecodeKeystore: %v", err)
	}

	line := strings.TrimSpace(out.String())

	path := strings.TrimPrefix(line, "ANDROID_KEYSTORE_PATH=")
	if line == path || !filepath.IsAbs(path) {
		t.Fatalf("out = %q, want absolute ANDROID_KEYSTORE_PATH", line)
	}

	if path != fsys.Path("project/release.keystore") {
		t.Fatalf("path = %q, want %q", path, fsys.Path("project/release.keystore"))
	}
}

// TestAndroidDecodeKeystore_Refusals separates the two ways the keystore
// can be unusable. They carry different sentinels on purpose: an absent
// secret is a permissions problem the operator resolves in the forge
// (EX_NOPERM), while a secret that is present but not decodable is
// malformed input. Neither may leave a partial keystore on disk.
func TestAndroidDecodeKeystore_Refusals(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		base64  string
		wantErr error
	}{
		{name: "no secret at all", wantErr: errs.ErrPermissionDenied},
		{name: "secret is not base64", base64: "not!valid!base64", wantErr: errs.ErrMalformedInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fsys := testfs.NewReal(t)

			var out, stderr bytes.Buffer

			err := appbuild.AndroidDecodeKeystore(&out, &stderr, appbuild.AndroidDecodeKeystoreInput{
				Base64: tc.base64,
				Dir:    fsys.Root,
			})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}

			if _, statErr := os.Stat(filepath.Join(fsys.Root, "release.keystore")); !os.IsNotExist(statErr) {
				t.Errorf("left a keystore behind (stat err = %v)", statErr)
			}

			// No ANDROID_KEYSTORE_PATH line: the workflow redirects w to
			// $GITHUB_ENV, so a path emitted here would point gradle at a
			// keystore that was never written.
			if out.Len() != 0 {
				t.Errorf("emitted %q on a refused run", out.String())
			}

			if stderr.Len() != 0 {
				t.Errorf("reported success on a refused run: %q", stderr.String())
			}
		})
	}
}

// TestAndroidDecodeKeystore_DefaultDirHonoursTempDir verifies the v4
// hardening: when no explicit Dir is supplied, the keystore lands under the
// run context's scratch dir rather than cwd. That removes the keystore from
// any `path: .` upload-artifact glob a caller might add later.
//
// The scratch dir arrives as Input.TempDir — the CLI's --temp-dir binds it to
// $CI_TEMP_DIR/$RUNNER_TEMP. It is passed in rather than set in the environment,
// so this test mutates no process state and can run in parallel.
func TestAndroidDecodeKeystore_DefaultDirHonoursTempDir(t *testing.T) {
	t.Parallel()

	runnerTemp := t.TempDir()

	var out bytes.Buffer

	body := []byte("keystore-bytes")
	encoded := base64.StdEncoding.EncodeToString(body)

	if err := appbuild.AndroidDecodeKeystore(&out, io.Discard, appbuild.AndroidDecodeKeystoreInput{
		Base64: encoded,
		// Dir intentionally empty — exercise the default path.
		TempDir: runnerTemp,
	}); err != nil {
		t.Fatal(err)
	}

	expectedPath := filepath.Join(runnerTemp, "release.keystore")
	if !strings.Contains(out.String(), "ANDROID_KEYSTORE_PATH="+expectedPath) {
		t.Errorf("default Dir should resolve to the run context temp dir; got: %s", out.String())
	}

	info, err := os.Stat(expectedPath)
	if err != nil {
		t.Fatalf("keystore missing at %s: %v", expectedPath, err)
	}

	if info.Mode().Perm() != 0o600 {
		t.Errorf("keystore mode = %o, want 0600", info.Mode().Perm())
	}
}

// TestAndroidDecodeKeystore_DefaultDirFallsBackToTempWhenRunnerTempUnset
// covers the local-dev / non-GitHub-runner case: RUNNER_TEMP is empty,
// so the helper falls back to os.MkdirTemp. The keystore must still
// end up OUTSIDE cwd.
func TestAndroidDecodeKeystore_DefaultDirFallsBackToTempWhenRunnerTempUnset(t *testing.T) {
	t.Setenv("RUNNER_TEMP", "")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer

	encoded := base64.StdEncoding.EncodeToString([]byte("x"))
	if err := appbuild.AndroidDecodeKeystore(&out, io.Discard, appbuild.AndroidDecodeKeystoreInput{
		Base64: encoded,
	}); err != nil {
		t.Fatal(err)
	}

	// Parse out the path the helper emitted.
	line := strings.TrimSpace(out.String())

	const prefix = "ANDROID_KEYSTORE_PATH="

	if !strings.HasPrefix(line, prefix) {
		t.Fatalf("unexpected output: %s", line)
	}

	got := strings.TrimPrefix(line, prefix)

	// The dir belongs to the production fallback (os.MkdirTemp), not to the
	// test, so t.TempDir cannot own it and nothing else will remove it.
	// Without this the test leaves a keystore in the host's temp on every
	// run. Guarded because this is an unconditional RemoveAll: only ever
	// clean a fresh dir under the temp root the fallback was told to use.
	keystoreDir := filepath.Dir(got)
	if !strings.HasPrefix(keystoreDir, filepath.Join(os.TempDir(), "reusable-ci-keystore-")) {
		t.Fatalf("fallback dir %q is not a fresh dir under %s; refusing to remove it", keystoreDir, os.TempDir())
	}

	t.Cleanup(func() { _ = os.RemoveAll(keystoreDir) })

	if strings.HasPrefix(got, wd+string(os.PathSeparator)) || got == filepath.Join(wd, "release.keystore") {
		t.Errorf("keystore landed inside cwd %s — must live outside the project working dir; got %s", wd, got)
	}
}

func TestAndroidWriteSecretsProperties_WritesDecodedFileAtMode0600(t *testing.T) {
	fsys := testfs.NewReal(t)
	body := []byte("FOO=bar\nBAZ=qux\n")

	var out bytes.Buffer
	if err := appbuild.AndroidWriteSecretsProperties(&out, appbuild.AndroidWriteSecretsPropertiesInput{
		Base64: base64.StdEncoding.EncodeToString(body),
		Dir:    fsys.Root,
	}); err != nil {
		t.Fatal(err)
	}

	path := fsys.Path("secrets.properties")

	got, err := os.ReadFile(path) //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, body) {
		t.Errorf("body = %q, want %q", got, body)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 0600", info.Mode().Perm())
	}

	if !strings.Contains(out.String(), "decoded successfully") {
		t.Errorf("missing success log: %s", out.String())
	}
}

// TestAndroidWriteSecretsProperties_NoSecretIsASkip covers the absent
// secret. A project with no secrets.properties configured must still
// build, so this is a skip rather than an error.
//
// Every whitespace-only value reaches the same branch, which is why the
// function's later "decoded to zero bytes" guard cannot fire -- see
// docs/open-questions.md.
func TestAndroidWriteSecretsProperties_NoSecretIsASkip(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, base64 string }{
		{name: "unset", base64: ""},
		{name: "whitespace only", base64: " \n\t "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fsys := testfs.NewReal(t)

			var out bytes.Buffer

			if err := appbuild.AndroidWriteSecretsProperties(&out, appbuild.AndroidWriteSecretsPropertiesInput{
				Base64: tc.base64,
				Dir:    fsys.Root,
			}); err != nil {
				t.Fatalf("err = %v, want a skip", err)
			}

			// An empty secrets.properties is not the same as none: the
			// gradle secrets plugin reads the file if it exists, so
			// writing an empty one would mask a missing secret rather
			// than leave the build to fail on the real cause.
			if _, err := os.Stat(fsys.Path("secrets.properties")); !os.IsNotExist(err) {
				t.Errorf("wrote secrets.properties with nothing to put in it (stat err = %v)", err)
			}

			if !strings.Contains(out.String(), "skipping") {
				t.Errorf("missing skip log: %s", out.String())
			}
		})
	}
}

func TestAndroidWriteSecretsProperties_RejectsInvalidBase64(t *testing.T) {
	t.Parallel()

	fsys := testfs.NewReal(t)

	var out bytes.Buffer

	err := appbuild.AndroidWriteSecretsProperties(&out, appbuild.AndroidWriteSecretsPropertiesInput{
		Base64: "!!!not-base64!!!",
		Dir:    fsys.Root,
	})
	if !errors.Is(err, errs.ErrMalformedInput) {
		t.Fatalf("err = %v, want ErrMalformedInput", err)
	}

	// A secret that is present but unusable is an error, unlike an absent
	// one -- so it must not be quietly downgraded to the skip above.
	if strings.Contains(out.String(), "skipping") {
		t.Errorf("reported a skip for a malformed secret: %s", out.String())
	}

	if _, statErr := os.Stat(fsys.Path("secrets.properties")); !os.IsNotExist(statErr) {
		t.Errorf("left a secrets.properties behind (stat err = %v)", statErr)
	}
}

func TestAndroidResolveBuildTasks_WritesTasksOutput(t *testing.T) {
	sink := fakeoutputsink.New(t)

	err := appbuild.AndroidResolveBuildTasks(context.Background(), sink, io.Discard, appbuild.AndroidResolveBuildTasksInput{
		Flavor: "fdroid", BuildTypes: "release", IncludeAAB: true, BuildModule: "app", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})
	if err != nil {
		t.Fatalf("AndroidResolveBuildTasks: %v", err)
	}

	if got := sink.Single("tasks"); got != "assembleFdroidRelease app:bundleFdroidRelease" {
		t.Errorf("tasks = %q", got)
	}
}

func TestAndroidResolveBuildTasks_UsesCustomModuleAndPrintsStderr(t *testing.T) {
	sink := fakeoutputsink.New(t)

	var stderr bytes.Buffer

	err := appbuild.AndroidResolveBuildTasks(context.Background(), sink, &stderr, appbuild.AndroidResolveBuildTasksInput{
		Flavor:      "demo", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		BuildTypes:  "release",
		IncludeAAB:  true,
		BuildModule: "mymodule",
	})
	if err != nil {
		t.Fatalf("AndroidResolveBuildTasks: %v", err)
	}

	if got := sink.Single("tasks"); got != "assembleDemoRelease mymodule:bundleDemoRelease" {
		t.Errorf("tasks = %q", got)
	}

	if got := strings.TrimSpace(stderr.String()); got != "Building with tasks: assembleDemoRelease mymodule:bundleDemoRelease" {
		t.Errorf("stderr = %q", got)
	}
}

func TestAndroidResolveBuildTasks_UsesOverride(t *testing.T) {
	sink := fakeoutputsink.New(t)

	var stderr bytes.Buffer

	err := appbuild.AndroidResolveBuildTasks(context.Background(), sink, &stderr, appbuild.AndroidResolveBuildTasksInput{
		Override: "customTask anotherTask",
	})
	if err != nil {
		t.Fatalf("AndroidResolveBuildTasks: %v", err)
	}

	if got := sink.Single("tasks"); got != "customTask anotherTask" {
		t.Errorf("tasks = %q", got)
	}

	if got := strings.TrimSpace(stderr.String()); got != "Building with explicit tasks: customTask anotherTask" {
		t.Errorf("stderr = %q", got)
	}
}

func TestAndroidGradleBuild_WithSkipTests(t *testing.T) {
	ops := &fakeGradle{}
	if err := appbuild.AndroidGradleBuild(context.Background(), ops, io.Discard, io.Discard, appbuild.AndroidGradleBuildInput{
		Tasks: "assembleRelease app:bundleRelease", SkipTests: true,
	}); err != nil {
		t.Fatal(err)
	}

	want := []string{"assembleRelease", "app:bundleRelease", "-x", "test"} //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	if !equalArgs(ops.args, want) {
		t.Errorf("args = %v, want %v", ops.args, want)
	}
}

func TestAndroidGradleBuild_WithoutSkipTestsRunsTasksDirectly(t *testing.T) {
	ops := &fakeGradle{}
	if err := appbuild.AndroidGradleBuild(context.Background(), ops, io.Discard, io.Discard, appbuild.AndroidGradleBuildInput{
		Tasks: "assembleDebug", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}); err != nil {
		t.Fatal(err)
	}

	if !equalArgs(ops.args, []string{"assembleDebug"}) {
		t.Errorf("args = %v, want %v", ops.args, []string{"assembleDebug"})
	}
}

func TestAndroidGradleBuild_RequiresTasks(t *testing.T) {
	if err := appbuild.AndroidGradleBuild(context.Background(), &fakeGradle{}, io.Discard, io.Discard, appbuild.AndroidGradleBuildInput{Tasks: "  "}); err == nil {
		t.Fatal("expected error")
	}
}

func TestAndroidListArtifacts_FindsApkAndAab(t *testing.T) {
	fsys := testfs.NewReal(t)
	root := fsys.Root
	module := "app"
	fsys.WriteFile(filepath.Join(module, "build", "outputs", "apk", "debug", "demo.apk"), []byte("apk"))
	fsys.WriteFile(filepath.Join(module, "build", "outputs", "bundle", "release", "demo.aab"), []byte("aab"))
	// Decoy file that should be ignored.
	fsys.WriteFile(filepath.Join(module, "build", "outputs", "apk", "debug", "manifest.json"), []byte("{}"))

	var out bytes.Buffer
	if err := appbuild.AndroidListArtifacts(&out, appbuild.AndroidListArtifactsInput{
		BuildModule: module, Root: root,
	}); err != nil {
		t.Fatal(err)
	}

	out0 := out.String()
	if !strings.Contains(out0, "Built artifacts:") {
		t.Errorf("missing header in out:\n%s", out0)
	}

	if !strings.Contains(out0, "demo.apk") || !strings.Contains(out0, "demo.aab") {
		t.Errorf("missing artifact path in out:\n%s", out0)
	}

	if strings.Contains(out0, "manifest.json") {
		t.Errorf("unexpected non-artifact file in out:\n%s", out0)
	}
}

func TestAndroidListArtifacts_NoArtifactsPrintsNotice(t *testing.T) {
	var out bytes.Buffer

	fsys := testfs.NewReal(t)

	err := appbuild.AndroidListArtifacts(&out, appbuild.AndroidListArtifactsInput{
		BuildModule: "app", Root: fsys.Root,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "No artifacts found") {
		t.Errorf("missing notice:\n%s", out.String())
	}
}
