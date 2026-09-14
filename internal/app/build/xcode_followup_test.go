// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
	"github.com/stretchr/testify/require"
)

func TestXcodeReleaseBuild_IPAMatchesExistingUploadLocation(t *testing.T) {
	for _, testCase := range []struct {
		name, path        string
		alsoDirect, valid bool
	}{
		{"direct", "build/export/App.ipa", false, true},
		{"nested", "build/export/nested/App.ipa", false, false},
		{"uppercase", "build/export/App.IPA", false, false},
		{"nested_uppercase", "build/export/nested/App.IPA", false, false},
		{"wrong_location", "build/other/App.ipa", false, false},
		{"nested_and_direct", "build/export/nested/App.ipa", true, false},
		{"uppercase_and_direct", "build/export/App.IPA", true, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fsys, in := xcodeReleaseFixture(t)
			ops := &fakeXcodeBuild{run: func(args []string) {
				if args[0] == "archive" {
					fsys.MkdirAll("checkout/build/app.xcarchive")
				} else {
					require.Equal(t, "-exportArchive", args[0])
					fsys.WriteFile(filepath.Join("checkout", testCase.path), []byte("fresh fake IPA"))

					if testCase.alsoDirect {
						fsys.WriteFile("checkout/build/export/Other.ipa", []byte("matching direct IPA"))
					}
				}
			}}
			sink, sec := fakeoutputsink.New(t), &fakeSecurity{}

			var stdout, stderr bytes.Buffer

			err := appbuild.XcodeReleaseBuild(t.Context(), sink, sec, ops, output.Annotator{}, &stdout, &stderr, in)
			// Six install calls, the search-list read, and the teardown delete.
			require.Len(t, sec.calls, 8)
			require.Len(t, ops.calls, 2)

			if testCase.valid {
				require.NoError(t, err)
				require.Equal(t, map[string]string{"ipa-name": "mobile-v2.0", "version": "2.0", "build": "200"}, sink.AllScalar())
				require.True(t, strings.HasSuffix(stdout.String(), "Built artifacts:\nbuild/app.xcarchive\nbuild/export/App.ipa\n"))
			} else {
				require.ErrorIs(t, err, errs.ErrValidation)
				require.Empty(t, sink.Keys())
				require.NotContains(t, stdout.String(), "Built artifacts:")
			}
		})
	}
}

func TestXcodeArchive_ProvidedXCConfigPreflight(t *testing.T) {
	for _, name := range []string{"missing", "directory", "relative", "absolute_outside_project", "leaf_symlink", "literal_dash"} {
		t.Run(name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			t.Chdir(fsys.MkdirAll("checkout"))
			fsys.MkdirAll("checkout/App.xcodeproj")

			path := "Config.xcconfig"

			valid := name != "missing" && name != "directory"
			switch name {
			case "directory":
				fsys.MkdirAll("checkout/Config.xcconfig")
			case "relative":
				fsys.WriteFile("checkout/Config.xcconfig", []byte("SETTING = value\n"))
			case "absolute_outside_project":
				path = fsys.WriteFile("shared/Config.xcconfig", []byte("SETTING = value\n"))
			case "leaf_symlink":
				target := fsys.WriteFile("shared/Config.xcconfig", []byte("SETTING = value\n"))
				require.NoError(t, os.Symlink(target, fsys.Path("checkout/Config.xcconfig")))
			case "literal_dash":
				path = "-"

				fsys.WriteFile("checkout/-", []byte("SETTING = value\n"))
			}

			before := ownedTree(t, fsys.Root)
			ops := &fakeXcodeBuild{}

			var out bytes.Buffer

			err := appbuild.XcodeArchive(t.Context(), ops, &out, &out, appbuild.XcodeArchiveInput{
				Project: "App.xcodeproj", Scheme: "App", Configuration: "Release", Destination: "generic/platform=iOS", XcconfigPath: path,
			})
			if valid {
				require.NoError(t, err)
				require.Len(t, ops.calls, 1)
				require.Equal(t, []string{"-xcconfig", path}, ops.calls[0][len(ops.calls[0])-2:])
			} else {
				require.Empty(t, ops.calls, "invalid xcconfig reached build port")
				require.Empty(t, out.String())
				require.True(t, maps.Equal(before, ownedTree(t, fsys.Root)))
				require.ErrorIs(t, err, errs.ErrMissingInput)
				require.ErrorContains(t, err, "xcconfig")
			}
		})
	}
}

func TestXcodeVersionInfo_EmptyDiscoveryDoesNotReadCWDDecoy(t *testing.T) {
	fsys := testfs.NewReal(t)
	t.Chdir(fsys.MkdirAll("cwd"))
	fsys.WriteFile("cwd/project.pbxproj", []byte("MARKETING_VERSION = 9.0;\nCURRENT_PROJECT_VERSION = 900;\n"))
	root := fsys.MkdirAll("selected-empty")

	for _, name := range []string{"empty_selected_root", "empty_default_root", "first_project_in_selected_root"} {
		t.Run(name, func(t *testing.T) {
			in := appbuild.XcodeVersionInfoInput{Root: root}
			want := map[string]string{"version": "unknown", "build": "unknown"}

			if name == "empty_default_root" {
				in.Root = ""
			}

			if name == "first_project_in_selected_root" {
				makeXcodeProject(t, fsys, "selected-empty/Zebra", "3.0", "300")
				makeXcodeProject(t, fsys, "selected-empty/Alpha", "2.0", "200")

				want = map[string]string{"version": "2.0", "build": "200"}
			}

			sink := fakeoutputsink.New(t)

			var stderr, annotations bytes.Buffer
			require.NoError(t, appbuild.XcodeVersionInfo(t.Context(), sink, &stderr, output.NewAnnotator(&annotations, output.FormatGitHub), in))
			require.Equal(t, want, sink.AllScalar())

			if name != "first_project_in_selected_root" {
				require.Empty(t, stderr.String())
				require.Equal(t, "::warning::Could not determine version from project file\n", annotations.String())
			}
		})
	}
}

func TestXcodeReleaseFixture_CanonicalizesOwnedOSAlias(t *testing.T) {
	outer := testfs.NewReal(t)
	actual, err := filepath.EvalSymlinks(outer.MkdirAll("actual-temp"))
	require.NoError(t, err)

	alias := outer.Path("temp-alias")
	require.NoError(t, os.Symlink(actual, alias))
	t.Setenv("TMPDIR", alias)
	t.Run("aliased_temp_parent", func(t *testing.T) {
		fsys, in := xcodeReleaseFixture(t)
		canonical, resolveErr := filepath.EvalSymlinks(fsys.Root)
		require.NoError(t, resolveErr)
		require.Equal(t, canonical, fsys.Root, "owned fixture root must be canonical before deriving paths")

		sec, ops := &fakeSecurity{err: errKeychainInUse}, &fakeXcodeBuild{}

		var out bytes.Buffer

		err := appbuild.XcodeReleaseBuild(t.Context(), fakeoutputsink.New(t), sec, ops, output.Annotator{}, &out, &out, in)
		require.ErrorIs(t, err, errKeychainInUse)
		require.Len(t, sec.calls, 1)
		require.Empty(t, ops.calls)
	})
}
