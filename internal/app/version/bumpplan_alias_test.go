// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

func TestBumpPlanAliases_PrimaryChangelogAliasesHaveNoEffects(t *testing.T) {
	for _, kind := range []string{"same-item", "cross-lexical", "cross-hardlink", "cargo-own-hardlink", "cargo-cross-hardlink", "xcode-absent-own", "xcode-absent-cross"} {
		for _, reverse := range []bool{false, true} {
			name := kind + "/primary-first"
			if reverse {
				name = kind + "/changelog-first"
			}

			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				t.Chdir(root)
				require.NoError(t, os.Mkdir("late", 0o700))
				require.NoError(t, os.Mkdir("metadata", 0o700))

				primary := pipeline.PlannedArtifact{Name: "primary", ProjectType: projecttype.Gradle, WorkingDirectory: "late"}
				metadata := pipeline.PlannedArtifact{Name: "metadata", ProjectType: projecttype.Meta, WorkingDirectory: "metadata"}
				xcconfig := ""

				switch kind {
				case "same-item":
					primary.Gradle = &config.GradleConfig{GradleVersionFile: "CHANGELOG.md"}
					metadata.WorkingDirectory = "late"

					writeFile(t, root, "late/CHANGELOG.md", "version=1.0.0\n")
				case "cross-lexical":
					primary.WorkingDirectory = ""
					primary.Gradle = &config.GradleConfig{GradleVersionFile: "./metadata//CHANGELOG.md"}
					metadata.WorkingDirectory = "./metadata//."

					writeFile(t, root, "metadata/CHANGELOG.md", "version=1.0.0\n")
				case "cross-hardlink":
					writeFile(t, root, "late/gradle.properties", "version=1.0.0\n")
					require.NoError(t, os.Link("late/gradle.properties", "metadata/CHANGELOG.md"))
				case "cargo-own-hardlink", "cargo-cross-hardlink":
					primary.ProjectType = projecttype.Cargo

					writeFile(t, root, "late/Cargo.toml", "[package]\nversion = \"1.0.0\"\n")
					writeFile(t, root, "late/Cargo.lock", "late lock canary\n")

					destination := "metadata/CHANGELOG.md"
					if kind == "cargo-own-hardlink" {
						destination = "late/CHANGELOG.md"
					}

					require.NoError(t, os.Link("late/Cargo.toml", destination))
				case "xcode-absent-own":
					primary.ProjectType = projecttype.XcodeIOS
					xcconfig = "./CHANGELOG.md"
				case "xcode-absent-cross":
					primary.ProjectType = projecttype.XcodeIOS
					primary.WorkingDirectory = "."
					xcconfig = "./metadata//CHANGELOG.md"
				}

				items := []pipeline.PlannedArtifact{primary, metadata}
				if reverse {
					slices.Reverse(items)
				}

				assertBumpPlanAliasRefusal(t, root, items, xcconfig, "full.md", "primary version target/changelog destination alias")
			})
		}
	}
}

func TestBumpPlanAliases_CargoPropertyAliasesHaveNoEffects(t *testing.T) {
	for _, pt := range []projecttype.Type{projecttype.Gradle, projecttype.GradleAndroid, projecttype.XcodeIOS} {
		for _, alias := range []string{"same-path", "hardlink"} {
			for _, reverse := range []bool{false, true} {
				name := string(pt) + "/" + alias + "/cargo-first"
				if reverse {
					name = string(pt) + "/" + alias + "/property-first"
				}

				t.Run(name, func(t *testing.T) {
					root := t.TempDir()
					t.Chdir(root)
					require.NoError(t, os.Mkdir("late", 0o700))
					writeFile(t, root, "late/Cargo.toml", "[package]\nname = \"fixture\"\nversion = \"1.0.0\"\n")
					writeFile(t, root, "late/Cargo.lock", "late lock canary\n")

					file := "./Cargo.toml"
					if alias == "hardlink" {
						file = "release.properties"
						require.NoError(t, os.Link("late/Cargo.toml", "late/"+file))
					}

					items := []pipeline.PlannedArtifact{
						{Name: "rust", ProjectType: projecttype.Cargo, WorkingDirectory: "./late//."},
						{Name: "property", ProjectType: pt, WorkingDirectory: "late", Gradle: &config.GradleConfig{GradleVersionFile: file}, GradleAndroid: &config.GradleAndroidConfig{GradleVersionFile: file}},
					}
					if reverse {
						slices.Reverse(items)
					}

					assertBumpPlanAliasRefusal(t, root, items, file, "full.md", "incompatible primary version writers")
				})
			}
		}
	}
}

func aliasPlanInput(t *testing.T, items []pipeline.PlannedArtifact, xcconfig string) appversion.BumpPlanInput {
	t.Helper()

	body, err := json.Marshal(pipeline.ReleasePrepareStagePlan{
		Version: pipeline.ReleasePlanVersion, Stage: "prepare",
		Targets: pipeline.ReleasePrepareTargets{VersionBump: pipeline.TargetPlan[pipeline.PlannedArtifact]{Runs: true, Items: items}},
	})
	require.NoError(t, err)

	return appversion.BumpPlanInput{PlanJSON: string(body), Version: "  v2.0.0  ", ChangelogFile: "full.md", XcconfigFile: xcconfig}
}

func assertBumpPlanAliasRefusal(t *testing.T, root string, items []pipeline.PlannedArtifact, xcconfig, changelog, reason string) {
	t.Helper()
	// Every bad suffix follows real engine mutation and all native adapter ports.
	for _, dir := range []string{"first", "java", "web", "early-rust"} {
		require.NoError(t, os.Mkdir(dir, 0o700))
		writeFile(t, root, dir+"/CHANGELOG.md", "old "+dir+" changes\n")
	}

	for path, body := range map[string]string{
		"full.md": "# New changes\n", "canary": "untouched\n", "first/gradle.properties": "version=1.0.0\nKEEP=yes\n",
		"java/release.xml": "<project/>\n", "web/.npmrc": "prefix=../selected-web\n",
		"early-rust/Cargo.toml": "[package]\nversion = \"1.0.0\"\n", "early-rust/Cargo.lock": "early lock canary\n",
	} {
		writeFile(t, root, path, body)
		require.NoError(t, os.Chmod(path, 0o640)) //nolint:gosec // nondefault mode is a preservation canary on this test's owned files.
	}

	items = append([]pipeline.PlannedArtifact{
		{Name: "first", ProjectType: projecttype.Gradle, WorkingDirectory: "first"},
		{Name: "java", ProjectType: projecttype.Maven, WorkingDirectory: "java"},
		{Name: "web", ProjectType: projecttype.NPM, WorkingDirectory: "web"},
		{Name: "early-rust", ProjectType: projecttype.Cargo, WorkingDirectory: "early-rust"},
	}, items...)
	in := aliasPlanInput(t, items, xcconfig)
	in.ChangelogFile = changelog
	in.MavenCLIOpts = []string{"-f", "release.xml"}
	npm, maven, cargo := &fakeNPMOps{}, &fakeMavenOps{}, &fakeCargoOps{avail: true}
	sink := fakeoutputsink.New(t)
	require.NoError(t, sink.Set(t.Context(), "file-pattern", "seeded pattern"))
	require.NoError(t, sink.Set(t.Context(), "canary", "seeded output"))
	before, beforeOutputs := bumpOwnedTree(t, root), sink.AllScalar()
	identities := map[string]os.FileInfo{}

	for path, state := range before {
		if state.mode.IsRegular() || state.mode&os.ModeSymlink != 0 {
			info, err := os.Lstat(path)
			require.NoError(t, err)

			identities[path] = info
		}
	}

	var stdout, stderr, diagnostics bytes.Buffer

	err := appversion.BumpPlan(t.Context(), appversion.BumpOps{Maven: maven, NPM: npm, Cargo: cargo}, sink, &stdout, &stderr, output.NewAnnotator(&diagnostics, output.FormatGitHub), in)
	assert.ErrorIs(t, err, errs.ErrValidation) //nolint:testifylint // still check every no-effects assertion when refusal is broken.
	assert.ErrorContains(t, err, reason)       //nolint:testifylint // distinguish the correct preflight refusal from a post-write Cargo failure.
	assert.Equal(t, before, bumpOwnedTree(t, root), "complete owned bytes and modes, including source and lockfiles")

	for path, info := range identities {
		after, statErr := os.Lstat(path)
		require.NoError(t, statErr)
		assert.True(t, os.SameFile(info, after), "identity changed: %s", path)
	}

	assert.Equal(t, beforeOutputs, sink.AllScalar())
	assert.Empty(t, stdout.String())
	assert.Empty(t, stderr.String())
	assert.Empty(t, diagnostics.String())
	assert.Zero(t, maven.calls)
	assert.Empty(t, maven.args)
	assert.Zero(t, npm.calls)
	assert.Empty(t, npm.args)
	assert.Empty(t, cargo.calls)
	assert.Zero(t, cargo.availabilityChecks)
}

func TestBumpPlanAliases_CompatiblePropertySharingAndRetries(t *testing.T) {
	for _, alias := range []string{"same-path", "hardlink", "distinct-same-content"} {
		for _, reverse := range []bool{false, true} {
			name := alias + "/forward"
			if reverse {
				name = alias + "/reverse"
			}

			t.Run(name, func(t *testing.T) {
				assertBumpPlanPropertySharing(t, alias, reverse)
			})
		}
	}
}

func assertBumpPlanPropertySharing(t *testing.T, alias string, reverse bool) {
	t.Helper()
	root := t.TempDir()
	t.Chdir(root)

	const (
		original = "version=1.0.0\nversionName=1.0.0\nversionCode=9\nMARKETING_VERSION = 1.0.0\nKEEP=yes\n"
		shared   = "version=2.0.0\nversionName=2.0.0\nversionCode=10\nMARKETING_VERSION = 2.0.0\nKEEP=yes\n"
	)

	writeFile(t, root, "CHANGELOG.md", "# Source is also destination\n")
	writeFile(t, root, "shared.properties", original)

	expected := map[string]string{"shared.properties": shared}
	if alias == "distinct-same-content" {
		expected["shared.properties"] = original
	}

	items := make([]pipeline.PlannedArtifact, 0, 7)

	for _, tc := range []struct {
		pt   projecttype.Type
		want string
	}{
		{projecttype.Gradle, "version=2.0.0\nversionName=1.0.0\nversionCode=9\nMARKETING_VERSION = 1.0.0\nKEEP=yes\n"},
		{projecttype.GradleAndroid, "version=1.0.0\nversionName=2.0.0\nversionCode=10\nMARKETING_VERSION = 1.0.0\nKEEP=yes\n"},
		{projecttype.XcodeIOS, "version=1.0.0\nversionName=1.0.0\nversionCode=9\nMARKETING_VERSION = 2.0.0\nKEEP=yes\n"},
	} {
		file := "shared.properties"
		if alias != "same-path" {
			file = string(tc.pt) + ".properties"
			if alias == "hardlink" {
				require.NoError(t, os.Link("shared.properties", file))
				expected[file] = shared
			} else {
				writeFile(t, root, file, original)
				expected[file] = tc.want
			}
		}

		items = append(items, pipeline.PlannedArtifact{Name: string(tc.pt), ProjectType: tc.pt, Gradle: &config.GradleConfig{GradleVersionFile: file}, GradleAndroid: &config.GradleAndroidConfig{GradleVersionFile: file}})
	}

	xcconfig := "shared.properties"
	if alias != "same-path" {
		xcconfig = string(projecttype.XcodeIOS) + ".properties"
	}

	if reverse {
		slices.Reverse(items)
	}

	items = append(items, slices.Clone(items)...)
	items = append(items, pipeline.PlannedArtifact{Name: "meta", ProjectType: projecttype.Meta, WorkingDirectory: "."})
	in := aliasPlanInput(t, items, xcconfig)
	in.ChangelogFile = ""

	want := bumpOwnedTree(t, root)
	for path, body := range expected {
		state := want[path]
		state.body = body
		want[path] = state
	}

	var out bytes.Buffer

	sink := fakeoutputsink.New(t)
	for range 2 {
		require.NoError(t, appversion.BumpPlan(t.Context(), appversion.BumpOps{}, sink, &out, &out, output.Annotator{}, in))
		require.Equal(t, want, bumpOwnedTree(t, root))
		require.NotEmpty(t, sink.Single("file-pattern"))
	}

	if alias == "hardlink" {
		base, err := os.Stat("shared.properties")
		require.NoError(t, err)

		for _, pt := range []projecttype.Type{projecttype.Gradle, projecttype.GradleAndroid, projecttype.XcodeIOS} {
			info, statErr := os.Stat(string(pt) + ".properties")
			require.NoError(t, statErr)
			require.True(t, os.SameFile(base, info))
		}
	}
}

func TestBumpPlanAliases_DuplicatesCreationAndNativeOverrides(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	require.NoError(t, os.Mkdir("other", 0o700))

	const manifest = "[package]\nname = \"fixture\"\nversion = \"1.0.0\"\n"
	for path, body := range map[string]string{
		"CHANGELOG.md": "# Shared source and destinations\n", "Cargo.toml": manifest, "release.properties": manifest,
		"Cargo.lock": "root lock canary\n", "other/Cargo.lock": "other lock canary\n",
		"release.xml": "<project/>\n", ".npmrc": "prefix=selected-web\n",
	} {
		writeFile(t, root, path, body)
	}

	require.NoError(t, os.Link("Cargo.toml", "other/Cargo.toml"))
	require.NoError(t, os.Link("CHANGELOG.md", "other/CHANGELOG.md"))

	rust := pipeline.PlannedArtifact{Name: "rust", ProjectType: projecttype.Cargo, WorkingDirectory: "."}
	in := aliasPlanInput(t, []pipeline.PlannedArtifact{
		rust, rust, {Name: "linked-rust", ProjectType: projecttype.Cargo, WorkingDirectory: "other"},
		{Name: "jvm", ProjectType: projecttype.Gradle, Gradle: &config.GradleConfig{GradleVersionFile: "release.properties"}},
		{Name: "apple", ProjectType: projecttype.XcodeIOS}, {Name: "java", ProjectType: projecttype.Maven}, {Name: "web", ProjectType: projecttype.NPM},
	}, "")
	in.ChangelogFile = ""
	in.MavenCLIOpts = []string{"--file=release.xml"}

	want := bumpOwnedTree(t, root)
	for _, path := range []string{"Cargo.toml", "other/Cargo.toml"} {
		state := want[path]
		state.body = "[package]\nname = \"fixture\"\nversion = \"2.0.0\"\n"
		want[path] = state
	}

	// Read as gradle.properties, the manifest's spaced `version = "1.0.0"`
	// is the version property and is rewritten in place, not duplicated.
	state := want["release.properties"]
	state.body = "[package]\nname = \"fixture\"\nversion=2.0.0\n"
	want["release.properties"] = state
	want["versions.xcconfig"] = bumpFileState{mode: 0o644, body: "MARKETING_VERSION = 2.0.0\n"}
	maven, npm, cargo := &fakeMavenOps{}, &fakeNPMOps{}, &fakeCargoOps{avail: true, failOne: true}
	sink := fakeoutputsink.New(t)

	var out bytes.Buffer
	require.NoError(t, appversion.BumpPlan(t.Context(), appversion.BumpOps{Maven: maven, NPM: npm, Cargo: cargo}, sink, &out, &out, output.Annotator{}, in))
	require.Equal(t, want, bumpOwnedTree(t, root))
	require.NotEmpty(t, sink.Single("file-pattern"))
	require.Equal(t, 1, maven.calls)
	require.Equal(t, ".", maven.dir)
	require.Equal(t, []string{"--file=release.xml", "versions:set", "-DnewVersion=2.0.0", "-DgenerateBackupPoms=false", "-DprocessAllModules=true", "-DskipTests"}, maven.args)
	require.Equal(t, 1, npm.calls)
	require.Equal(t, ".", npm.dir)
	require.Equal(t, []string{"version", "2.0.0", "--no-git-tag-version", "--allow-same-version"}, npm.args)
	require.Equal(t, 3, cargo.availabilityChecks)
	require.Equal(t, [][]string{{"update", "--workspace", "--offline"}, {"update", "--workspace"}, {"update", "--workspace", "--offline"}, {"update", "--workspace", "--offline"}}, cargo.calls)

	for _, path := range []string{"Cargo.toml", "CHANGELOG.md"} {
		base, err := os.Stat(path)
		require.NoError(t, err)
		linked, err := os.Stat("other/" + path)
		require.NoError(t, err)
		require.True(t, os.SameFile(base, linked))
	}
}

func TestBumpPlanAliases_SourceOnlyPrimaryAliasHasNoEffects(t *testing.T) {
	for _, alias := range []string{"same-path", "hardlink", "symlink", "symlink-hardlink"} {
		t.Run(alias, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			require.NoError(t, os.Mkdir("late", 0o700))
			writeFile(t, root, "late/gradle.properties", "# Source changes\nversion=1.0.0\n")
			require.NoError(t, os.Chmod("late/gradle.properties", 0o640)) //nolint:gosec // owned source-mode preservation canary.

			primary := pipeline.PlannedArtifact{Name: "source-alias", ProjectType: projecttype.Gradle, WorkingDirectory: "late"}
			source := "./late//gradle.properties"

			switch alias {
			case "same-path":
			case "hardlink":
				source = "source.md"
				require.NoError(t, os.Link("late/gradle.properties", source))
			case "symlink":
				source = "source.md"
				require.NoError(t, os.Symlink("late/gradle.properties", source))
			case "symlink-hardlink":
				source = "source.md"

				require.NoError(t, os.Link("late/gradle.properties", "source-target.md"))
				require.NoError(t, os.Symlink("source-target.md", source))
			}

			assertBumpPlanAliasRefusal(t, root, []pipeline.PlannedArtifact{primary}, "", source, "primary version target/changelog source alias")
		})
	}
}

func TestBumpPlanAliases_NonprimarySourceSymlinksRemainSupported(t *testing.T) {
	for _, target := range []string{"full.md", "CHANGELOG.md"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			writeFile(t, root, target, "# Source changes\n")
			writeFile(t, root, "gradle.properties", "version=1.0.0\n")
			require.NoError(t, os.Symlink(target, "source.md"))

			link, err := os.Lstat("source.md")
			require.NoError(t, err)
			source, err := os.Stat("source.md")
			require.NoError(t, err)
			want := bumpOwnedTree(t, root)
			state := want["gradle.properties"]
			state.body = "version=2.0.0\n"

			want["gradle.properties"] = state
			if target == "full.md" {
				want["CHANGELOG.md"] = bumpFileState{mode: 0o644, body: "# Source changes\n"}
			}

			in := aliasPlanInput(t, []pipeline.PlannedArtifact{{Name: "jvm", ProjectType: projecttype.Gradle}}, "")
			in.ChangelogFile = "source.md"
			sink := fakeoutputsink.New(t)

			var out bytes.Buffer
			require.NoError(t, appversion.BumpPlan(t.Context(), appversion.BumpOps{}, sink, &out, &out, output.Annotator{}, in))
			require.Equal(t, want, bumpOwnedTree(t, root))
			require.NotEmpty(t, sink.Single("file-pattern"))

			afterLink, err := os.Lstat("source.md")
			require.NoError(t, err)
			require.True(t, os.SameFile(link, afterLink))

			afterSource, err := os.Stat("source.md")
			require.NoError(t, err)
			require.True(t, os.SameFile(source, afterSource))
		})
	}
}
