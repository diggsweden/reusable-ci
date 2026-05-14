// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

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
	if sink.Single("version") != "1.2.3" || sink.Single("version-code") != "42" {
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
	if sink.Single("version") != "unknown" {
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
	var stdout, stderr bytes.Buffer
	err := appbuild.AndroidDecodeKeystore(&stdout, io.Discard, appbuild.AndroidDecodeKeystoreInput{
		Base64: enc, Dir: dir,
	})
	if err != nil {
		t.Fatalf("AndroidDecodeKeystore: %v", err)
	}
	want := filepath.Join(dir, "release.keystore")
	if got := strings.TrimSpace(stdout.String()); got != "ANDROID_KEYSTORE_PATH="+want {
		t.Errorf("stdout = %q, want %q", got, "ANDROID_KEYSTORE_PATH="+want)
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

func TestAndroidDecodeKeystore_RejectsEmptySecret(t *testing.T) {
	fsys := testfs.NewReal(t)
	err := appbuild.AndroidDecodeKeystore(io.Discard, io.Discard, appbuild.AndroidDecodeKeystoreInput{Dir: fsys.Root})
	if err == nil || !strings.Contains(err.Error(), "ANDROID_KEYSTORE") {
		t.Errorf("expected secret-required error, got %v", err)
	}
}

func TestAndroidResolveBuildTasks_WritesTasksOutput(t *testing.T) {
	sink := fakeoutputsink.New(t)
	err := appbuild.AndroidResolveBuildTasks(context.Background(), sink, io.Discard, appbuild.AndroidResolveBuildTasksInput{
		Flavor: "fdroid", BuildTypes: "release", IncludeAAB: true, BuildModule: "app",
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
		Flavor:      "demo",
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

func TestAndroidGradleBuild_WithSkipTests(t *testing.T) {
	ops := &fakeGradle{}
	if err := appbuild.AndroidGradleBuild(context.Background(), ops, io.Discard, io.Discard, appbuild.AndroidGradleBuildInput{
		Tasks: "assembleRelease app:bundleRelease", SkipTests: true,
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{"assembleRelease", "app:bundleRelease", "-x", "test"}
	if !equalArgs(ops.args, want) {
		t.Errorf("args = %v, want %v", ops.args, want)
	}
}

func TestAndroidGradleBuild_WithoutSkipTestsRunsTasksDirectly(t *testing.T) {
	ops := &fakeGradle{}
	if err := appbuild.AndroidGradleBuild(context.Background(), ops, io.Discard, io.Discard, appbuild.AndroidGradleBuildInput{
		Tasks: "assembleDebug",
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

	var stdout bytes.Buffer
	if err := appbuild.AndroidListArtifacts(&stdout, appbuild.AndroidListArtifactsInput{
		BuildModule: module, Root: root,
	}); err != nil {
		t.Fatal(err)
	}
	out0 := stdout.String()
	if !strings.Contains(out0, "Built artifacts:") {
		t.Errorf("missing header in stdout:\n%s", out0)
	}
	if !strings.Contains(out0, "demo.apk") || !strings.Contains(out0, "demo.aab") {
		t.Errorf("missing artifact path in stdout:\n%s", out0)
	}
	if strings.Contains(out0, "manifest.json") {
		t.Errorf("unexpected non-artifact file in stdout:\n%s", out0)
	}
}

func TestAndroidListArtifacts_NoArtifactsPrintsNotice(t *testing.T) {
	var stdout bytes.Buffer
	fsys := testfs.NewReal(t)
	err := appbuild.AndroidListArtifacts(&stdout, appbuild.AndroidListArtifactsInput{
		BuildModule: "app", Root: fsys.Root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "No artifacts found") {
		t.Errorf("missing notice:\n%s", stdout.String())
	}
}
