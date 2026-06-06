// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestAndroidArtifactNames_OverrideMode(t *testing.T) {
	sink := fakeoutputsink.New(t)

	var stderr bytes.Buffer

	err := appbuild.AndroidArtifactNames(context.Background(), sink, &stderr, appbuild.AndroidArtifactNamesInput{
		Override: "myapp-1.2.3",
		RepoName: "ignored-when-override-set",
	})
	if err != nil {
		t.Fatalf("AndroidArtifactNames: %v", err)
	}

	want := map[string]string{
		"debug-name":   "myapp-1.2.3-debug",
		"release-name": "myapp-1.2.3-release",
		"aab-name":     "myapp-1.2.3",
		"sbom-name":    "myapp-1.2.3-sbom",
	}
	for k, v := range want {
		if got := sink.Single(k); got != v {
			t.Errorf("output %s = %q, want %q", k, got, v)
		}
	}
}

func TestAndroidArtifactNames_DateAndPrefix(t *testing.T) {
	sink := fakeoutputsink.New(t)

	err := appbuild.AndroidArtifactNames(context.Background(), sink, io.Discard, appbuild.AndroidArtifactNamesInput{
		IncludeDate: true,
		Prefix:      "Nightly",
		RepoName:    "demo-app",
		Today:       time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AndroidArtifactNames: %v", err)
	}

	if got := sink.Single("debug-name"); got != "2026-05-10 - Nightly - demo-app - APK debug" {
		t.Errorf("debug-name = %q", got)
	}
}

func TestAndroidArtifactNames_OverrideIgnoresDatePrefixAndFlavor(t *testing.T) {
	sink := fakeoutputsink.New(t)

	err := appbuild.AndroidArtifactNames(context.Background(), sink, io.Discard, appbuild.AndroidArtifactNamesInput{
		IncludeDate: true,
		Prefix:      "ci",
		RepoName:    "myapp",
		Flavor:      "prod",
		Override:    "wallet-android-demo",
		Today:       time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AndroidArtifactNames: %v", err)
	}

	want := map[string]string{
		"debug-name":   "wallet-android-demo-debug",
		"release-name": "wallet-android-demo-release",
		"aab-name":     "wallet-android-demo",
		"sbom-name":    "wallet-android-demo-sbom",
	}
	for key, wantValue := range want {
		if got := sink.Single(key); got != wantValue {
			t.Errorf("%s = %q, want %q", key, got, wantValue)
		}
	}
}

func TestAndroidVersionInfo_ReadsGradleProperties(t *testing.T) {
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.WriteFile("gradle.properties", []byte("versionName=1.2.3\nversionCode=42\n"))

	sink := fakeoutputsink.New(t)
	if err := appbuild.AndroidVersionInfo(context.Background(), sink, io.Discard, output.Annotator{}, appbuild.AndroidVersionInfoInput{Dir: dir}); err != nil {
		t.Fatalf("AndroidVersionInfo: %v", err)
	}

	if sink.Single("version") != "1.2.3" || sink.Single("version-code") != "42" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("version=%q version-code=%q", sink.Single("version"), sink.Single("version-code"))
	}
}

func TestAndroidVersionInfo_MissingFileFallsBackToUnknown(t *testing.T) {
	fsys := testfs.NewReal(t)
	dir := fsys.Root // no gradle.properties
	sink := fakeoutputsink.New(t)

	var stderr bytes.Buffer
	if err := appbuild.AndroidVersionInfo(context.Background(), sink, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appbuild.AndroidVersionInfoInput{Dir: dir}); err != nil {
		t.Fatalf("AndroidVersionInfo: %v", err)
	}

	if sink.Single("version") != "unknown" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("version = %q, want unknown", sink.Single("version"))
	}

	if sink.Single("version-code") != "unknown" {
		t.Errorf("version-code = %q, want unknown", sink.Single("version-code"))
	}

	if !strings.Contains(stderr.String(), "gradle.properties not found") {
		t.Errorf("expected warning in stderr: %q", stderr.String())
	}
}

func TestAndroidVersionInfo_PrintsExactVersionLine(t *testing.T) {
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.WriteFile("gradle.properties", []byte("versionName=1.0.0-beta1\nversionCode=5\n"))

	sink := fakeoutputsink.New(t)

	var stderr bytes.Buffer
	if err := appbuild.AndroidVersionInfo(context.Background(), sink, &stderr, output.Annotator{}, appbuild.AndroidVersionInfoInput{Dir: dir}); err != nil {
		t.Fatalf("AndroidVersionInfo: %v", err)
	}

	if got := sink.Single("version"); got != "1.0.0-beta1" {
		t.Errorf("version = %q", got)
	}

	if got := sink.Single("version-code"); got != "5" {
		t.Errorf("version-code = %q", got)
	}

	if got := strings.TrimSpace(stderr.String()); got != "Version: 1.0.0-beta1 (5)" {
		t.Errorf("stderr = %q", got)
	}
}

func TestAndroidDecodeKeystore_WritesFileAndPrintsEnvLine(t *testing.T) {
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	body := []byte("\x01\x02fake-keystore-bytes\x03")
	enc := base64.StdEncoding.EncodeToString(body)

	var out, stderr bytes.Buffer

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

	info, _ := os.Stat(want)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("keystore mode = %o, want 0o600", info.Mode().Perm())
	}

	_ = stderr
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

func TestAndroidDecodeKeystore_RejectsEmptySecret(t *testing.T) {
	fsys := testfs.NewReal(t)

	err := appbuild.AndroidDecodeKeystore(io.Discard, io.Discard, appbuild.AndroidDecodeKeystoreInput{Dir: fsys.Root})
	if err == nil || !strings.Contains(err.Error(), "ANDROID_KEYSTORE") {
		t.Errorf("expected secret-required error, got %v", err)
	}
}

// TestAndroidDecodeKeystore_DefaultDirHonoursRunnerTemp verifies the
// v4 hardening: when no explicit Dir is supplied, the keystore lands
// under $RUNNER_TEMP rather than cwd. Removes the keystore from any
// `path: .` upload-artifact glob a caller might add later.
func TestAndroidDecodeKeystore_DefaultDirHonoursRunnerTemp(t *testing.T) {
	runnerTemp := t.TempDir()
	t.Setenv("RUNNER_TEMP", runnerTemp)

	var out bytes.Buffer

	body := []byte("keystore-bytes")
	encoded := base64.StdEncoding.EncodeToString(body)

	if err := appbuild.AndroidDecodeKeystore(&out, io.Discard, appbuild.AndroidDecodeKeystoreInput{
		Base64: encoded,
		// Dir intentionally empty — exercise the default path.
	}); err != nil {
		t.Fatal(err)
	}

	expectedPath := filepath.Join(runnerTemp, "release.keystore")
	if !strings.Contains(out.String(), "ANDROID_KEYSTORE_PATH="+expectedPath) {
		t.Errorf("default Dir should resolve to $RUNNER_TEMP; got: %s", out.String())
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

func TestAndroidWriteSecretsProperties_EmptyBase64IsNoop(t *testing.T) {
	fsys := testfs.NewReal(t)

	var out bytes.Buffer
	if err := appbuild.AndroidWriteSecretsProperties(&out, appbuild.AndroidWriteSecretsPropertiesInput{
		Dir: fsys.Root,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(fsys.Path("secrets.properties")); !os.IsNotExist(err) {
		t.Errorf("secrets.properties should not be written when base64 is empty (err=%v)", err)
	}

	if !strings.Contains(out.String(), "skipping") {
		t.Errorf("missing skip log: %s", out.String())
	}
}

func TestAndroidWriteSecretsProperties_RejectsInvalidBase64(t *testing.T) {
	fsys := testfs.NewReal(t)

	err := appbuild.AndroidWriteSecretsProperties(io.Discard, appbuild.AndroidWriteSecretsPropertiesInput{
		Base64: "!!!not-base64!!!",
		Dir:    fsys.Root,
	})
	if err == nil || !strings.Contains(err.Error(), "decode secrets.properties") {
		t.Fatalf("err = %v, want decode error", err)
	}
}

func TestAndroidWriteSecretsProperties_RejectsEmptyDecoded(t *testing.T) {
	fsys := testfs.NewReal(t)
	err := appbuild.AndroidWriteSecretsProperties(io.Discard, appbuild.AndroidWriteSecretsPropertiesInput{
		Base64: base64.StdEncoding.EncodeToString(nil),
		Dir:    fsys.Root,
	})
	// Empty body after decode is treated as "no secrets configured" (skip).
	if err != nil {
		t.Fatalf("err = %v, want skip", err)
	}

	if _, err := os.Stat(fsys.Path("secrets.properties")); !os.IsNotExist(err) {
		t.Errorf("empty body should not be written")
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
