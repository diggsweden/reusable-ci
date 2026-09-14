// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"os"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
	"github.com/stretchr/testify/require"
)

func TestXcodeReleaseBuild_PreflightRefusesOccupiedReleaseOutputs(t *testing.T) {
	for _, name := range []string{"archive_directory", "archive_file", "archive_symlink", "build_file", "build_symlink", "export_file", "export_symlink", "export_nonempty"} {
		t.Run(name, func(t *testing.T) {
			fsys, in := xcodeReleaseFixture(t)
			fsys.MkdirAll("checkout/build")
			fsys.WriteFile("outside/canary", []byte("untouched outside output root"))

			switch name {
			case "archive_directory":
				fsys.WriteFile("checkout/build/app.xcarchive/Info.plist", []byte("old archive"))
			case "archive_file":
				fsys.WriteFile("checkout/build/app.xcarchive", []byte("not a bundle"))
			case "archive_symlink":
				require.NoError(t, os.Symlink(fsys.Path("outside"), fsys.Path("checkout/build/app.xcarchive")))
			case "build_file", "build_symlink":
				require.NoError(t, os.Remove(fsys.Path("checkout/build")))

				if name == "build_file" {
					fsys.WriteFile("checkout/build", []byte("not a directory"))
				} else {
					require.NoError(t, os.Symlink(fsys.Path("outside"), fsys.Path("checkout/build")))
				}
			case "export_file":
				fsys.WriteFile("checkout/build/export", []byte("not a directory"))
			case "export_symlink":
				require.NoError(t, os.Symlink(fsys.Path("outside"), fsys.Path("checkout/build/export")))
			case "export_nonempty":
				fsys.WriteFile("checkout/build/export/previous.ipa", []byte("stale IPA"))
			}

			assertXcodePreflightRefusal(t, fsys, in, errs.ErrValidation, "release outputs")
		})
	}
}

func TestXcodeReleaseBuild_RequiresFreshExpectedArtifactsBeforePublication(t *testing.T) {
	for _, name := range []string{"missing_archive", "wrong_archive_location", "archive_file", "archive_symlink", "build_symlink", "missing_IPA", "wrong_IPA_location", "empty_IPA", "multiple_IPA", "IPA_directory", "IPA_symlink", "IPA_parent_symlink", "export_symlink"} {
		t.Run(name, func(t *testing.T) {
			fsys, in := xcodeReleaseFixture(t)
			fsys.WriteFile("outside/canary", []byte("outside canary"))
			fsys.WriteFile("outside/app.ipa", []byte("outside IPA"))
			outside := ownedTree(t, fsys.Path("outside"))
			ops := &fakeXcodeBuild{run: func(args []string) {
				if args[0] == "archive" {
					switch name {
					case "missing_archive":
						return
					case "wrong_archive_location":
						fsys.MkdirAll("checkout/build/other.xcarchive")
					case "archive_file":
						fsys.WriteFile("checkout/build/app.xcarchive", []byte("not a bundle"))
					case "archive_symlink":
						require.NoError(t, os.Symlink(fsys.Path("outside"), fsys.Path("checkout/build/app.xcarchive")))
					case "build_symlink":
						require.NoError(t, os.Remove(fsys.Path("checkout/build")))
						require.NoError(t, os.Symlink(fsys.Path("outside"), fsys.Path("checkout/build")))
					default:
						fsys.WriteFile("checkout/build/app.xcarchive/Info.plist", []byte("fresh archive"))
					}

					return
				}

				switch name {
				case "missing_IPA":
				case "wrong_IPA_location":
					fsys.WriteFile("checkout/build/wrong/app.ipa", []byte("misplaced"))
				case "empty_IPA":
					fsys.WriteFile("checkout/build/export/app.ipa", nil)
				case "multiple_IPA":
					fsys.WriteFile("checkout/build/export/app.ipa", []byte("first"))
					fsys.WriteFile("checkout/build/export/second.ipa", []byte("second"))
				case "IPA_directory":
					fsys.MkdirAll("checkout/build/export/app.ipa")
				case "IPA_symlink":
					fsys.MkdirAll("checkout/build/export")
					require.NoError(t, os.Symlink(fsys.Path("outside/app.ipa"), fsys.Path("checkout/build/export/app.ipa")))
				case "IPA_parent_symlink":
					fsys.MkdirAll("checkout/build/export")
					require.NoError(t, os.Symlink(fsys.Path("outside"), fsys.Path("checkout/build/export/nested")))
				case "export_symlink":
					require.NoError(t, os.Symlink(fsys.Path("outside"), fsys.Path("checkout/build/export")))
				}
			}}
			sink, sec := fakeoutputsink.New(t), &fakeSecurity{}

			var stdout, stderr bytes.Buffer

			err := appbuild.XcodeReleaseBuild(t.Context(), sink, sec, ops, output.Annotator{}, &stdout, &stderr, in)
			require.Empty(t, sink.Keys(), "invalid artifact published success")
			require.NotContains(t, stdout.String(), "Built artifacts:")
			require.ErrorIs(t, err, errs.ErrValidation)

			wantCalls := 2
			cause := "export IPA artifacts:"

			if name == "missing_archive" || name == "wrong_archive_location" || name == "archive_file" || name == "archive_symlink" || name == "build_symlink" {
				wantCalls = 1
				cause = "expected fresh archive directory"
			}

			require.ErrorContains(t, err, cause)
			require.Len(t, ops.calls, wantCalls)
			// Six install calls, the search-list read, and the teardown delete.
			require.Len(t, sec.calls, 8)
			require.Equal(t, outside, ownedTree(t, fsys.Path("outside")))
		})
	}
}

// TestXcodeReleaseBuild_ArchiveBundleContainment covers what the archive
// carries rather than only that it exists. The directory is published as a
// release artifact, so a link inside it that leaves the bundle either breaks
// where it is unpacked or resolves to something else there; the archive would
// look complete either way. Apple bundles rely on relative links of their own,
// so those stay supported and only escaping ones are refused.
func TestXcodeReleaseBuild_ArchiveBundleContainment(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		arrange func(t *testing.T, fsys *testfs.Real)
		refused bool
	}{
		{
			name: "a framework's own version link",
			arrange: func(t *testing.T, fsys *testfs.Real) {
				t.Helper()
				fsys.WriteFile("checkout/build/app.xcarchive/Products/App.app/Frameworks/Example.framework/Versions/A/Example", []byte("inert framework"))
				require.NoError(t, os.Symlink("A", fsys.Path("checkout/build/app.xcarchive/Products/App.app/Frameworks/Example.framework/Versions/Current")))
			},
		},
		{
			name: "a relative link deeper into the bundle",
			arrange: func(t *testing.T, fsys *testfs.Real) {
				t.Helper()
				fsys.WriteFile("checkout/build/app.xcarchive/Products/App.app/Info.plist", []byte("inert plist"))
				require.NoError(t, os.Symlink("Products/App.app/Info.plist", fsys.Path("checkout/build/app.xcarchive/Alias.plist")))
			},
		},
		{
			// Climbing is not escaping: this one lands back inside the bundle,
			// which is only visible if the target is resolved from the link's
			// own directory rather than from the bundle root.
			name: "a nested link climbing back to a sibling inside the bundle",
			arrange: func(t *testing.T, fsys *testfs.Real) {
				t.Helper()
				fsys.MkdirAll("checkout/build/app.xcarchive/Products/App.app")
				require.NoError(t, os.Symlink("../../Info.plist", fsys.Path("checkout/build/app.xcarchive/Products/App.app/Info.plist")))
			},
		},
		{
			name: "an absolute link out of the bundle",
			arrange: func(t *testing.T, fsys *testfs.Real) {
				t.Helper()
				fsys.WriteFile("outside/secret", []byte("caller data"))
				require.NoError(t, os.Symlink(fsys.Path("outside/secret"), fsys.Path("checkout/build/app.xcarchive/Payload")))
			},
			refused: true,
		},
		{
			name: "a relative link climbing out of the bundle",
			arrange: func(t *testing.T, fsys *testfs.Real) {
				t.Helper()
				fsys.WriteFile("checkout/build/elsewhere", []byte("not part of the archive"))
				require.NoError(t, os.Symlink("../elsewhere", fsys.Path("checkout/build/app.xcarchive/Payload")))
			},
			refused: true,
		},
		{
			name: "a nested relative link climbing out of the bundle",
			arrange: func(t *testing.T, fsys *testfs.Real) {
				t.Helper()
				fsys.WriteFile("checkout/build/elsewhere", []byte("not part of the archive"))
				fsys.MkdirAll("checkout/build/app.xcarchive/Products/App.app")
				require.NoError(t, os.Symlink("../../../elsewhere", fsys.Path("checkout/build/app.xcarchive/Products/App.app/Payload")))
			},
			refused: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fsys, in := xcodeReleaseFixture(t)
			in.EnableCodeSigning = false
			sink := fakeoutputsink.New(t)
			ops := &fakeXcodeBuild{run: func([]string) {
				fsys.WriteFile("checkout/build/app.xcarchive/Info.plist", []byte("owned synthetic archive"))
				testCase.arrange(t, fsys)
			}}

			var stdout, stderr bytes.Buffer

			err := appbuild.XcodeReleaseBuild(t.Context(), sink, unsignedXcodeSecurity{t}, ops, output.Annotator{}, &stdout, &stderr, in)

			if testCase.refused {
				require.ErrorIs(t, err, errs.ErrValidation)
				require.ErrorContains(t, err, "archive bundle link")
				require.Empty(t, sink.Keys(), "a refused archive must not be published")
				require.NotContains(t, stdout.String(), "Built artifacts:")

				return
			}

			require.NoError(t, err)
			require.Contains(t, stdout.String(), "Built artifacts:\nbuild/app.xcarchive\n")
		})
	}
}
