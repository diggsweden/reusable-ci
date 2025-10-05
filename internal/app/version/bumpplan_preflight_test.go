// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type bumpFileState struct {
	mode fs.FileMode
	body string
}

func bumpOwnedTree(t *testing.T, root string) map[string]bumpFileState {
	t.Helper()

	state := map[string]bumpFileState{}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		item := bumpFileState{mode: info.Mode()}
		switch {
		case info.Mode().IsRegular():
			body, readErr := os.ReadFile(path) //nolint:gosec // snapshot only this test's owned tree; links are recorded, never followed, and no concurrent writer exists.
			if readErr != nil {
				return readErr
			}

			item.body = string(body)
		case info.Mode()&os.ModeSymlink != 0:
			target, readErr := os.Readlink(path)
			if readErr != nil {
				return readErr
			}

			item.body = target
		}

		state[rel] = item

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return state
}

func TestBumpPlanPreflight_LateLocalFailureHasNoEffects(t *testing.T) {
	for _, kind := range []string{"missing-gradle", "missing-custom-gradle", "missing-cargo", "cargo-no-section", "cargo-no-version", "changelog-directory", "changelog-link", "changelog-internal-link", "version-link", "version-internal-link", "parent-link", "escaping-override", "gradle-parent", "xcconfig-parent", "xcconfig-directory", "nil-npm", "nil-maven"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)

			for _, dir := range []string{"first", "web", "late", "outside"} {
				require.NoError(t, os.Mkdir(dir, 0o700))
			}

			for path, body := range map[string]string{
				"full.md": "new changelog\n", "first/CHANGELOG.md": "old first\n", "first/gradle.properties": "version=1.0.0\n",
				"web/CHANGELOG.md": "old web\n", "web/package.json": `{"name":"web","version":"1.0.0"}`,
				"late/CHANGELOG.md": "old late\n", "outside/canary": "outside unchanged\n",
			} {
				writeFile(t, root, path, body)
			}

			late := pipeline.PlannedArtifact{Name: "late", ProjectType: projecttype.Gradle, WorkingDirectory: "late"}
			npm, maven := &retryNPM{}, &fakeMavenOps{}
			ops := appversion.BumpOps{NPM: npm, Maven: maven}
			in := appversion.BumpPlanInput{Version: "2.0.0", ChangelogFile: "full.md"}
			want := errs.ErrMissingInput

			switch kind {
			case "missing-gradle":
			case "missing-custom-gradle":
				late.Gradle = &config.GradleConfig{GradleVersionFile: "missing.properties"}

				writeFile(t, root, "late/gradle.properties", "version=default-canary\n")
			case "missing-cargo":
				late.ProjectType = projecttype.Cargo
			case "cargo-no-section", "cargo-no-version":
				late.ProjectType = projecttype.Cargo

				body := "[dependencies]\n"
				if kind == "cargo-no-version" {
					body = "[package]\nname = \"late\"\n"
				}

				writeFile(t, root, "late/Cargo.toml", body)

				want = errs.ErrValidation
			case "changelog-directory", "changelog-link":
				require.NoError(t, os.Remove("late/CHANGELOG.md"))

				if kind == "changelog-directory" {
					require.NoError(t, os.Mkdir("late/CHANGELOG.md", 0o700))
				} else {
					require.NoError(t, os.Symlink("../outside/canary", "late/CHANGELOG.md"))
				}

				writeFile(t, root, "late/gradle.properties", "version=1.0.0\n")

				want = errs.ErrValidation
			case "version-link":
				require.NoError(t, os.Symlink("../outside/canary", "late/gradle.properties"))

				want = errs.ErrValidation
			case "changelog-internal-link":
				writeFile(t, root, "late/gradle.properties", "version=1.0.0\n")
				require.NoError(t, os.Remove("late/CHANGELOG.md"))
				require.NoError(t, os.Symlink("gradle.properties", "late/CHANGELOG.md"))

				want = errs.ErrValidation
			case "version-internal-link":
				writeFile(t, root, "late/actual.properties", "version=1.0.0\n")
				require.NoError(t, os.Symlink("actual.properties", "late/gradle.properties"))

				want = errs.ErrValidation
			case "parent-link":
				require.NoError(t, os.Symlink("../outside", "late/linked"))

				late.Gradle = &config.GradleConfig{GradleVersionFile: "linked/canary"}
				want = errs.ErrValidation
			case "escaping-override":
				late.Gradle = &config.GradleConfig{GradleVersionFile: "../outside/canary"}
				want = errs.ErrUsage
			case "xcconfig-parent":
				late.ProjectType = projecttype.XcodeIOS
				in.XcconfigFile = "missing/versions.xcconfig"
			case "gradle-parent":
				late.Gradle = &config.GradleConfig{GradleVersionFile: "missing/release.properties"}
			case "xcconfig-directory":
				late.ProjectType = projecttype.XcodeIOS

				require.NoError(t, os.Mkdir("late/versions.xcconfig", 0o700))

				want = errs.ErrValidation
			case "nil-npm":
				ops.NPM = nil
				want = errs.ErrUsage
			case "nil-maven":
				late.ProjectType = projecttype.Maven

				writeFile(t, root, "late/pom.xml", "<project/>\n")

				ops.Maven = nil
				want = errs.ErrUsage
			}

			plan := pipeline.ReleasePrepareStagePlan{Version: pipeline.ReleasePlanVersion, Stage: "prepare", Targets: pipeline.ReleasePrepareTargets{VersionBump: pipeline.TargetPlan[pipeline.PlannedArtifact]{Runs: true, Items: []pipeline.PlannedArtifact{
				{Name: "first", ProjectType: projecttype.Gradle, WorkingDirectory: "first"}, {Name: "web", ProjectType: projecttype.NPM, WorkingDirectory: "web"}, late,
			}}}}
			body, err := json.Marshal(plan)
			require.NoError(t, err)

			in.PlanJSON = string(body)
			sink := fakeoutputsink.New(t)
			require.NoError(t, sink.Set(t.Context(), "seed", "unchanged"))
			before := bumpOwnedTree(t, root)
			beforeOutputs := sink.AllScalar()

			var out bytes.Buffer

			err = appversion.BumpPlan(t.Context(), ops, sink, &out, &out, output.NewAnnotator(&out, output.FormatGitHub), in)
			require.ErrorIs(t, err, want)

			if kind == "gradle-parent" || kind == "xcconfig-parent" {
				require.ErrorIs(t, err, os.ErrNotExist)
			}

			artifact := "late"
			if kind == "nil-npm" {
				artifact = "web"
			}

			require.Contains(t, err.Error(), `artifact "`+artifact+`"`)
			require.Equal(t, before, bumpOwnedTree(t, root))
			require.Equal(t, beforeOutputs, sink.AllScalar())
			require.Empty(t, out.String())
			require.Zero(t, npm.calls)
			require.Empty(t, maven.args)
		})
	}
}

func TestBumpPlanPreflight_ValidMixedPlan(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	for _, dir := range []string{"gradle", "android", "xcode", "cargo", "maven", "web"} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	for path, body := range map[string]string{"full.md": "new changes\n", "gradle/custom.properties": "version=1.0.0\n", "android/gradle.properties": "versionName=1.0.0\nversionCode=9\n", "cargo/Cargo.toml": "[package]\nname = \"fixture\"\nversion = \"1.0.0\"\n", "maven/pom.xml": "<project/>", "web/package.json": `{"name":"web","version":"1.0.0"}`} {
		writeFile(t, root, path, body)
	}

	plan := pipeline.ReleasePrepareStagePlan{Version: pipeline.ReleasePlanVersion, Stage: "prepare", Targets: pipeline.ReleasePrepareTargets{VersionBump: pipeline.TargetPlan[pipeline.PlannedArtifact]{Runs: true, Items: []pipeline.PlannedArtifact{
		{Name: "jvm", ProjectType: projecttype.Gradle, WorkingDirectory: "gradle", Gradle: &config.GradleConfig{GradleVersionFile: "custom.properties"}},
		{Name: "mobile", ProjectType: projecttype.GradleAndroid, WorkingDirectory: "android"},
		{Name: "apple", ProjectType: projecttype.XcodeIOS, WorkingDirectory: "xcode"},
		{Name: "rust", ProjectType: projecttype.Cargo, WorkingDirectory: "cargo"},
		{Name: "java", ProjectType: projecttype.Maven, WorkingDirectory: "maven"},
		{Name: "js", ProjectType: projecttype.NPM, WorkingDirectory: "web"},
	}}}}

	body, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}

	npm, maven := &retryNPM{}, &fakeMavenOps{}
	sink := fakeoutputsink.New(t)

	var out bytes.Buffer

	err = appversion.BumpPlan(t.Context(), appversion.BumpOps{NPM: npm, Maven: maven}, sink, &out, &out, output.Annotator{}, appversion.BumpPlanInput{PlanJSON: string(body), Version: "2.0.0", ChangelogFile: "full.md"})
	if err != nil {
		t.Fatal(err)
	}

	if npm.calls != 1 || len(maven.args) == 0 || sink.Single("file-pattern") == "" {
		t.Fatalf("positive did not reach all effects: npm=%d maven=%v", npm.calls, maven.args)
	}

	for path, want := range map[string]string{"gradle/custom.properties": "version=2.0.0\n", "android/gradle.properties": "versionName=2.0.0\nversionCode=10\n", "xcode/versions.xcconfig": "MARKETING_VERSION = 2.0.0\n", "cargo/Cargo.toml": "[package]\nname = \"fixture\"\nversion = \"2.0.0\"\n"} {
		if got := readTestFile(t, path); got != want {
			t.Errorf("%s=%q want=%q", path, got, want)
		}
	}

	for _, dir := range []string{"gradle", "android", "xcode", "cargo", "maven", "web"} {
		if got := readTestFile(t, filepath.Join(dir, "CHANGELOG.md")); got != "new changes\n" {
			t.Errorf("%s changelog=%q", dir, got)
		}
	}
}

func TestBumpPlanPreflight_PreservesMavenTargetSelection(t *testing.T) {
	for _, opts := range [][]string{{"-f", "release.xml"}, {"--file=release.xml"}, {"-frelease.xml"}, {"-f", "module"}} {
		root := t.TempDir()
		t.Chdir(root)

		if err := os.Mkdir("module", 0o700); err != nil {
			t.Fatal(err)
		}

		writeFile(t, root, "full.md", "new changes\n")
		writeFile(t, root, "release.xml", "<project/>\n")
		writeFile(t, root, "module/pom.xml", "<project/>\n")

		plan := pipeline.ReleasePrepareStagePlan{Version: pipeline.ReleasePlanVersion, Stage: "prepare", Targets: pipeline.ReleasePrepareTargets{VersionBump: pipeline.TargetPlan[pipeline.PlannedArtifact]{Runs: true, Items: []pipeline.PlannedArtifact{{Name: "selected-pom", ProjectType: projecttype.Maven, WorkingDirectory: "."}}}}}

		body, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}

		maven := &fakeMavenOps{}

		var out bytes.Buffer

		err = appversion.BumpPlan(t.Context(), appversion.BumpOps{Maven: maven}, fakeoutputsink.New(t), &out, &out, output.Annotator{}, appversion.BumpPlanInput{PlanJSON: string(body), Version: "2.0.0", ChangelogFile: "full.md", MavenCLIOpts: opts})
		if err != nil {
			t.Fatal(err)
		}

		want := append(slices.Clone(opts), "versions:set", "-DnewVersion=2.0.0", "-DgenerateBackupPoms=false", "-DprocessAllModules=true", "-DskipTests")
		if !slices.Equal(maven.args, want) || maven.dir != "." {
			t.Fatalf("args=%v dir=%q", maven.args, maven.dir)
		}
	}
}

func TestBumpPlanPreflight_UnrepresentableInputsHaveNoEffects(t *testing.T) {
	for _, pt := range []projecttype.Type{projecttype.Gradle, projecttype.GradleAndroid, projecttype.XcodeIOS, projecttype.Cargo} {
		for _, value := range []string{"2.0.0\nINJECTED=true", "2.0.0\rINJECTED=true", "2.0.0\x00INJECTED=true", "\n2.0.0", "2.0.0\r\n", "2.0.0\xff"} {
			t.Run(string(pt)+"/version/"+value, func(t *testing.T) {
				testBumpPlanUnrepresentable(t, pt, value, "", "late")
			})
		}
	}

	for _, pt := range []projecttype.Type{projecttype.Gradle, projecttype.GradleAndroid, projecttype.XcodeIOS} {
		for _, path := range []string{"release version", "release\tversion", "release\nversion", "release\rversion", "release\vversion", "release\fversion", "release\u00a0version", "release\x00version", " release", "release "} {
			t.Run(string(pt)+"/path/"+path, func(t *testing.T) {
				testBumpPlanUnrepresentable(t, pt, "2.0.0", path, "late")
			})
		}
	}

	for _, pt := range []projecttype.Type{projecttype.Gradle, projecttype.GradleAndroid, projecttype.XcodeIOS} {
		for _, path := range []string{"release*.properties", "release?.properties", "release[1].properties", `release\name.properties`, ":(literal)release.properties", ":(glob)release.properties", ":(exclude)release.properties", ":!release.properties", "./:(glob)release.properties"} {
			for _, dir := range []string{".", "late"} {
				t.Run(string(pt)+"/git-path/"+dir+"/"+path, func(t *testing.T) {
					testBumpPlanUnrepresentable(t, pt, "2.0.0", path, dir)
				})
			}
		}
	}

	for _, dir := range []string{"late project", "late\u00a0project", "late\x00project", " late", "late ", "\tlate", "late\n", "\u00a0late", "late\u00a0", " ", "\t", "\u00a0", "late /..", "late*", "late?", "late[1]", `late\name`, ":(glob)late", ":(literal)late", ":!late", "./:(glob)late"} {
		t.Run("directory/"+dir, func(t *testing.T) {
			testBumpPlanUnrepresentable(t, projecttype.Go, "2.0.0", "", dir)
		})
	}
}

func testBumpPlanUnrepresentable(t *testing.T, pt projecttype.Type, ver, override, dir string) {
	t.Helper()
	root := t.TempDir()
	t.Chdir(root)

	for _, name := range []string{"first", "late"} {
		require.NoError(t, os.Mkdir(name, 0o700))
	}

	if dir != "late" && dir != "." && !strings.ContainsRune(dir, '\x00') {
		require.NoError(t, os.MkdirAll(dir, 0o700))
		writeFile(t, root, filepath.Join(dir, "CHANGELOG.md"), "raw directory canary\n")
	}

	if override != "" && !strings.ContainsRune(override, '\x00') {
		writeFile(t, root, filepath.Join(dir, override), "version=1.0.0\nversionName=1.0.0\nversionCode=7\nMARKETING_VERSION = 1.0.0\n")
	}

	for path, contents := range map[string]string{
		"full.md": "new changes\n", "CHANGELOG.md": "root canary\n", "first/CHANGELOG.md": "old first\n", "late/CHANGELOG.md": "old late\n",
		"late/gradle.properties": "version=1.0.0\nversionName=1.0.0\nversionCode=7\n",
		"late/Cargo.toml":        "[package]\nversion = \"1.0.0\"\n", "late/Cargo.lock": "lock canary\n",
	} {
		writeFile(t, root, path, contents)
	}

	late := pipeline.PlannedArtifact{Name: "late", ProjectType: pt, WorkingDirectory: dir,
		Gradle: &config.GradleConfig{GradleVersionFile: override}, GradleAndroid: &config.GradleAndroidConfig{GradleVersionFile: override}}
	plan := pipeline.ReleasePrepareStagePlan{Version: pipeline.ReleasePlanVersion, Stage: "prepare", Targets: pipeline.ReleasePrepareTargets{VersionBump: pipeline.TargetPlan[pipeline.PlannedArtifact]{Runs: true, Items: []pipeline.PlannedArtifact{
		{Name: "first", ProjectType: projecttype.NPM, WorkingDirectory: "first"}, late,
	}}}}
	body, err := json.Marshal(plan)
	require.NoError(t, err)

	maven, npm, cargo := &fakeMavenOps{}, &fakeNPMOps{}, &fakeCargoOps{avail: true}
	sink := fakeoutputsink.New(t)
	require.NoError(t, sink.Set(t.Context(), "file-pattern", "seed"))
	before, beforeOutputs := bumpOwnedTree(t, root), sink.AllScalar()

	var out bytes.Buffer

	err = appversion.BumpPlan(t.Context(), appversion.BumpOps{Maven: maven, NPM: npm, Cargo: cargo}, sink, &out, &out, output.NewAnnotator(&out, output.FormatGitHub), appversion.BumpPlanInput{
		PlanJSON: string(body), Version: ver, ChangelogFile: "full.md", XcconfigFile: override,
	})

	want := errs.ErrUsage
	if dir != "late" && dir != "." {
		want = errs.ErrInvalidConfig
	}

	require.ErrorIs(t, err, want)
	require.Equal(t, before, bumpOwnedTree(t, root))
	require.Equal(t, beforeOutputs, sink.AllScalar())
	require.Empty(t, out.String())
	require.Empty(t, maven.args)
	require.Empty(t, npm.args)
	require.Zero(t, maven.calls)
	require.Zero(t, npm.calls)
	require.Empty(t, cargo.calls)
	require.Zero(t, cargo.availabilityChecks)
}
