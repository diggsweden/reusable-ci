// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

func TestBumpPlan_AppliesAllItemsAndEmitsOneAggregatePathspec(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"api", "worker"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	changelog := filepath.Join(root, "full.md")
	if err := os.WriteFile(changelog, []byte("# Changes\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Chdir(root)

	plan := pipeline.ReleasePrepareStagePlan{
		Version: pipeline.ReleasePlanVersion,
		Stage:   "prepare",
		Targets: pipeline.ReleasePrepareTargets{VersionBump: pipeline.TargetPlan[pipeline.PlannedArtifact]{
			Runs: true,
			Items: []pipeline.PlannedArtifact{
				{Name: "api", ProjectType: projecttype.Go, WorkingDirectory: "api"},
				{Name: "worker", ProjectType: projecttype.Meta, WorkingDirectory: "worker"},
			},
		}},
	}

	body, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}

	sink := fakeoutputsink.New(t)
	if err := appversion.BumpPlan(context.Background(), appversion.BumpOps{}, sink, io.Discard, io.Discard, output.Annotator{}, appversion.BumpPlanInput{
		PlanJSON:      string(body),
		Version:       "1.2.3",
		ChangelogFile: changelog,
	}); err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{"api", "worker"} {
		got, readErr := os.ReadFile(filepath.Join(root, dir, "CHANGELOG.md"))
		if readErr != nil || string(got) != "# Changes\n" {
			t.Fatalf("%s changelog = %q, %v", dir, got, readErr)
		}
	}

	want := "api/CHANGELOG.md worker/CHANGELOG.md"
	if got := sink.Single("file-pattern"); got != want {
		t.Fatalf("file-pattern = %q, want %q", got, want)
	}
}

func TestBumpPlan_EngineTargetsExactChangeSet(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	for _, dir := range []string{"config", "apps/android/config", "apps/ios/config", "rust", "java", "web"} {
		require.NoError(t, os.MkdirAll(dir, 0o700))
	}

	for path, body := range map[string]string{
		"full.md": "new changes\n", "CHANGELOG.md": "old changes\n", "unrelated.txt": "keep\n",
		"config/release.properties": "version=1.0.0\nkeep=yes\n", "gradle.properties": "version=default-canary\n",
		"apps/android/config/android.release": "versionName=1.0.0\nversionCode=9\n", "apps/android/gradle.properties": "versionName=default-canary\n",
		"apps/ios/versions.xcconfig": "MARKETING_VERSION = default-canary\n",
		"rust/Cargo.toml":            "[package]\nname = \"fixture\"\nversion = \"1.0.0\"\n",
		"java/release.xml":           "<project/>\n", "web/.npmrc": "prefix=../selected-web\n",
	} {
		writeFile(t, root, path, body)
	}

	jvm := pipeline.PlannedArtifact{Name: "jvm", ProjectType: projecttype.Gradle, WorkingDirectory: "", Gradle: &config.GradleConfig{GradleVersionFile: "./config//release.properties"}}
	plan := pipeline.ReleasePrepareStagePlan{Version: pipeline.ReleasePlanVersion, Stage: "prepare",
		FilePattern: "custom.txt CHANGELOG.md custom.txt :(glob)java/**/pom.xml :(glob)custom/*.properties :(exclude)custom/private* :(literal)custom/[literal] custom/file?.txt",
		Targets: pipeline.ReleasePrepareTargets{VersionBump: pipeline.TargetPlan[pipeline.PlannedArtifact]{Runs: true, Items: []pipeline.PlannedArtifact{
			jvm,
			{Name: "mobile", ProjectType: projecttype.GradleAndroid, WorkingDirectory: "./apps//android/.", GradleAndroid: &config.GradleAndroidConfig{GradleVersionFile: "config/./android.release"}},
			{Name: "apple", ProjectType: projecttype.XcodeIOS, WorkingDirectory: "apps/ios"},
			{Name: "rust", ProjectType: projecttype.Cargo, WorkingDirectory: "rust"},
			{Name: "java", ProjectType: projecttype.Maven, WorkingDirectory: "java"},
			{Name: "web", ProjectType: projecttype.NPM, WorkingDirectory: "web"},
			jvm,
		}}},
	}
	body, err := json.Marshal(plan)
	require.NoError(t, err)

	wantTree := bumpOwnedTree(t, root)
	for path, contents := range map[string]string{
		"CHANGELOG.md": "new changes\n", "apps/android/CHANGELOG.md": "new changes\n", "apps/ios/CHANGELOG.md": "new changes\n",
		"rust/CHANGELOG.md": "new changes\n", "java/CHANGELOG.md": "new changes\n", "web/CHANGELOG.md": "new changes\n",
		"config/release.properties": "version=2.0.0\nkeep=yes\n", "apps/android/config/android.release": "versionName=2.0.0\nversionCode=10\n",
		"apps/ios/config/release.settings": "MARKETING_VERSION = 2.0.0\n", "rust/Cargo.toml": "[package]\nname = \"fixture\"\nversion = \"2.0.0\"\n",
	} {
		state, exists := wantTree[path]
		if !exists {
			state.mode = 0o644
		}

		state.body = contents
		wantTree[path] = state
	}

	maven, npm, cargo := &fakeMavenOps{}, &fakeNPMOps{}, &fakeCargoOps{avail: true}
	sink := fakeoutputsink.New(t)

	var out bytes.Buffer
	require.NoError(t, appversion.BumpPlan(t.Context(), appversion.BumpOps{Maven: maven, NPM: npm, Cargo: cargo}, sink, &out, &out, output.Annotator{}, appversion.BumpPlanInput{
		PlanJSON: string(body), Version: "  v2.0.0  ", ChangelogFile: "full.md", XcconfigFile: "./config//release.settings", MavenCLIOpts: []string{"-f", "release.xml"},
	}))
	require.Equal(t, wantTree, bumpOwnedTree(t, root))

	wantPaths := []string{
		"CHANGELOG.md", "gradle.properties", "build.gradle.kts", "settings.gradle.kts", "build.gradle", "settings.gradle", "config/release.properties",
		"apps/android/CHANGELOG.md", "apps/android/gradle.properties", "apps/android/build.gradle.kts", "apps/android/settings.gradle.kts", "apps/android/build.gradle", "apps/android/settings.gradle", "apps/android/config/android.release",
		"apps/ios/CHANGELOG.md", "apps/ios/versions.xcconfig", ":(glob)apps/ios/**/*.xcconfig", "apps/ios/config/release.settings",
		"rust/CHANGELOG.md", "rust/Cargo.toml", "rust/Cargo.lock", "java/CHANGELOG.md", ":(glob)java/**/pom.xml",
		"web/CHANGELOG.md", "web/package.json", "web/package-lock.json", "custom.txt",
		":(glob)custom/*.properties", ":(exclude)custom/private*", ":(literal)custom/[literal]", "custom/file?.txt",
	}
	require.Equal(t, strings.Join(wantPaths, " "), sink.Single("file-pattern"))
	require.Equal(t, []string{"-f", "release.xml", "versions:set", "-DnewVersion=2.0.0", "-DgenerateBackupPoms=false", "-DprocessAllModules=true", "-DskipTests"}, maven.args)
	require.Equal(t, "java", maven.dir)
	require.Equal(t, 1, maven.calls)
	require.Equal(t, []string{"version", "2.0.0", "--no-git-tag-version", "--allow-same-version"}, npm.args)
	require.Equal(t, "web", npm.dir)
	require.Equal(t, 1, npm.calls)
	require.Empty(t, cargo.calls)
	require.Zero(t, cargo.availabilityChecks)
	// This verifies the port handoff only, not Git pathspec expansion or a commit.
	repo := &fakeCommitPushRepo{hasStaged: true}
	require.NoError(t, appversion.CommitPush(t.Context(), repo, &out, appversion.CommitPushInput{
		Branch: "main", AuthorName: "fixture", AuthorEmail: "fixture@example.invalid", Message: "release", FilePattern: sink.Single("file-pattern"),
	}))
	require.Equal(t, wantPaths, repo.added)
}

func TestBumpPlan_GradleBackslashRetries(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	for _, dir := range []string{"jvm", "android"} {
		require.NoError(t, os.Mkdir(dir, 0o700))
	}

	for path, body := range map[string]string{
		"full.md": "new changes\n", "jvm/gradle.properties": "version=1.0.0\nKEEP=yes\n",
		"android/gradle.properties": "versionName=1.0.0\nversionCode=7\nKEEP=yes\n",
	} {
		writeFile(t, root, path, body)
	}

	plan := pipeline.ReleasePrepareStagePlan{Version: pipeline.ReleasePlanVersion, Stage: "prepare", Targets: pipeline.ReleasePrepareTargets{VersionBump: pipeline.TargetPlan[pipeline.PlannedArtifact]{Runs: true, Items: []pipeline.PlannedArtifact{
		{Name: "jvm", ProjectType: projecttype.Gradle, WorkingDirectory: "jvm"},
		{Name: "android", ProjectType: projecttype.GradleAndroid, WorkingDirectory: "android"},
	}}}}
	body, err := json.Marshal(plan)
	require.NoError(t, err)

	wantTree := bumpOwnedTree(t, root)
	for path, contents := range map[string]string{
		"jvm/gradle.properties":     "version=" + `release\\path\\` + "\nKEEP=yes\n",
		"android/gradle.properties": "versionName=" + `release\\path\\` + "\nversionCode=8\nKEEP=yes\n",
		"jvm/CHANGELOG.md":          "new changes\n", "android/CHANGELOG.md": "new changes\n",
	} {
		state, exists := wantTree[path]
		if !exists {
			state.mode = 0o644
		}

		state.body = contents
		wantTree[path] = state
	}

	sink := fakeoutputsink.New(t)
	require.NoError(t, sink.Close(t.Context()))

	for attempt := range 3 {
		err = appversion.BumpPlan(t.Context(), appversion.BumpOps{}, sink, io.Discard, io.Discard, output.Annotator{}, appversion.BumpPlanInput{
			PlanJSON: string(body), Version: `release\path\`, ChangelogFile: "full.md",
		})
		if attempt == 0 {
			require.ErrorContains(t, err, "emit file-pattern")
			sink = fakeoutputsink.New(t)
		} else {
			require.NoError(t, err)
		}

		require.Equal(t, wantTree, bumpOwnedTree(t, root))
	}
}
