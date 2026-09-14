// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/stretchr/testify/require"
)

type xcodeJourneySecurity struct {
	fakeSecurity
	observe func([]string)
}

func (sec *xcodeJourneySecurity) Run(ctx context.Context, args ...string) (string, error) {
	sec.observe(args)

	return sec.fakeSecurity.Run(ctx, args...)
}

type xcodeJourneySink struct {
	*fakeoutputsink.Sink
	events *[]string
}

func (sink *xcodeJourneySink) Set(ctx context.Context, key, value string) error {
	*sink.events = append(*sink.events, "output:"+key)

	return sink.Sink.Set(ctx, key, value)
}

func assertXcodeSecret(t *testing.T, path, want string) {
	t.Helper()

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, want, string(body))

	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular())
	require.EqualValues(t, 0o600, info.Mode().Perm())
}

func TestXcodeReleaseBuild_SignedJourneyPublishesOnlyValidatedReleaseSet(t *testing.T) { //nolint:gocognit // one event stream observes real signed setup, captured metadata, artifacts and final publication across selector variants.
	for _, name := range []string{"workspace_fresh", "project_empty_build", "workspace_empty_export", "explicit_missing_PBX", "blank_optional_xcconfig"} {
		t.Run(name, func(t *testing.T) {
			fsys, in := xcodeReleaseFixture(t)
			if name == "project_empty_build" || name == "explicit_missing_PBX" {
				in.Workspace, in.Project = "", "Sources/Zebra.xcodeproj"
			}

			if name == "explicit_missing_PBX" {
				require.NoError(t, os.Remove(fsys.Path("checkout/Sources/Zebra.xcodeproj/project.pbxproj")))
			}

			if name == "project_empty_build" {
				fsys.MkdirAll("checkout/build")
			}

			if name == "workspace_empty_export" {
				fsys.MkdirAll("checkout/build/export")
				fsys.WriteFile("checkout/build/unrelated/old.ipa", []byte("unrelated old IPA"))
				fsys.MkdirAll("checkout/build/unrelated/old.xcarchive")
				require.NoError(t, os.Chmod(fsys.Path("checkout/build/unrelated/old.ipa"), 0o640)) //nolint:gosec // deliberate owned canary mode.
			}

			if name == "blank_optional_xcconfig" {
				in.XCConfigBase64 = " \t\r\n"
			}

			var events []string

			sink := &xcodeJourneySink{Sink: fakeoutputsink.New(t), events: &events}
			sec := &xcodeJourneySecurity{observe: func(args []string) {
				events = append(events, args[0])
				if args[0] == "delete-keychain" {
					// Teardown runs after publication; the setup-phase invariants
					// below describe what must hold before any effect is visible.
					return
				}

				require.Empty(t, sink.Keys())
				assertXcodeSecret(t, fsys.Path("TMPDIR/certificate.p12"), "cert")
				assertXcodeSecret(t, fsys.Path("TMPDIR/pp.mobileprovision"), "profile")
				// Metadata must have been captured before the first external effect.
				makeXcodeProject(t, fsys, "checkout/Sources/Zebra", "8.0", "800")
			}}

			var xcconfig, plist string

			ops := &fakeXcodeBuild{run: func(args []string) {
				require.Empty(t, sink.Keys())
				assertXcodeSecret(t, fsys.Path("HOME/Library/MobileDevice/Provisioning Profiles/pp.mobileprovision"), "profile")

				events = append(events, args[0])
				if args[0] == "archive" {
					flag, identity := "-workspace", in.Workspace
					if in.Project != "" {
						flag, identity = "-project", in.Project
					}

					want := []string{"archive", flag, identity, "-scheme", "Zebra", "-configuration", "Release", "-archivePath", "build/app.xcarchive", "-destination", "generic/platform=iOS", "-skipPackagePluginValidation"}

					if name != "blank_optional_xcconfig" {
						require.Len(t, args, 15)
						xcconfig = args[13]
						require.Equal(t, fsys.Path("TMPDIR"), filepath.Dir(xcconfig))
						assertXcodeSecret(t, xcconfig, "SETTING = value\n")
						want = append(want, "-xcconfig", xcconfig)
					}

					want = append(want, "CURRENT_PROJECT_VERSION=999")
					require.Equal(t, want, args)
					fsys.WriteFile("checkout/build/app.xcarchive/Info.plist", []byte("owned synthetic archive"))
					// A legitimate in-bundle framework link is not an archive-root link.
					fsys.WriteFile("checkout/build/app.xcarchive/Products/App.app/Frameworks/Example.framework/Versions/A/Example", []byte("inert framework"))
					require.NoError(t, os.Symlink("A", fsys.Path("checkout/build/app.xcarchive/Products/App.app/Frameworks/Example.framework/Versions/Current")))

					return
				}

				require.Len(t, args, 7)
				plist = args[6]
				require.Equal(t, fsys.Path("TMPDIR"), filepath.Dir(filepath.Dir(plist)))
				assertXcodeSecret(t, plist, "<plist/>")
				require.Equal(t, []string{"-exportArchive", "-archivePath", "build/app.xcarchive", "-exportPath", "build/export", "-exportOptionsPlist", plist}, args)
				fsys.WriteFile("checkout/build/export/App.ipa", []byte("fresh IPA"))
			}}
			beforeEnv := os.Environ()

			var stdout, stderr, annotations bytes.Buffer

			err := appbuild.XcodeReleaseBuild(t.Context(), sink, sec, ops, output.NewAnnotator(&annotations, output.FormatGitHub), &stdout, &stderr, in)
			require.NoError(t, err)
			require.Equal(t, beforeEnv, os.Environ())
			require.Equal(t, []string{"create-keychain", "set-keychain-settings", "unlock-keychain", "import", "set-key-partition-list", "list-keychain", "list-keychain", "archive", "-exportArchive", "output:ipa-name", "output:version", "output:build", "delete-keychain"}, events,
				"teardown is the last thing a successful build does, after publication")

			keychain := fsys.Path("TMPDIR/app-signing.keychain-db")
			// The keychain password is minted by the run, not supplied: the
			// operator's secret must never reach a security(1) argv.
			generated := sec.calls[0][2]
			require.NotEqual(t, in.KeychainPassword, generated)
			require.GreaterOrEqual(t, len(generated), 32)
			require.Equal(t, [][]string{
				{"create-keychain", "-p", generated, keychain},
				{"set-keychain-settings", "-lut", "21600", keychain},
				{"unlock-keychain", "-p", generated, keychain},
				{"import", fsys.Path("TMPDIR/certificate.p12"), "-P", in.CertPassphrase, "-A", "-t", "cert", "-f", "pkcs12", "-k", keychain},
				{"set-key-partition-list", "-S", "apple-tool:,apple:", "-k", generated, keychain},
				{"list-keychain", "-d", "user"},
				{"list-keychain", "-d", "user", "-s", keychain},
				{"delete-keychain", keychain},
			}, sec.calls)

			want := map[string]string{"ipa-name": "mobile-v2.0", "version": "2.0", "build": "200"}
			if name == "explicit_missing_PBX" {
				want["version"], want["build"] = "unknown", "unknown"

				require.Empty(t, stderr.String())
				require.Equal(t, "::warning::Could not determine version from project file\n", annotations.String())
			} else {
				require.Equal(t, "Version: 2.0 (200)\n", stderr.String())
				require.Empty(t, annotations.String())
			}

			require.Equal(t, want, sink.AllScalar())

			_, listed, found := strings.Cut(stdout.String(), "Built artifacts:\n")
			require.True(t, found)
			require.Equal(t, "build/app.xcarchive\nbuild/export/App.ipa\n", listed)
			require.Equal(t, "fresh IPA", string(fsys.ReadFile("checkout/build/export/App.ipa")))

			if name == "workspace_empty_export" {
				require.Equal(t, "unrelated old IPA", string(fsys.ReadFile("checkout/build/unrelated/old.ipa")))
				info, statErr := os.Stat(fsys.Path("checkout/build/unrelated/old.ipa"))
				require.NoError(t, statErr)
				require.EqualValues(t, 0o640, info.Mode().Perm())
			}

			_, statErr := os.Stat(plist)
			require.ErrorIs(t, statErr, os.ErrNotExist)

			for _, secret := range []string{in.CertPassphrase, in.KeychainPassword, in.CertBase64, in.PPBase64, in.XCConfigBase64, in.ExportOptionsBase64} {
				require.NotContains(t, stdout.String()+stderr.String()+annotations.String(), secret)
			}
			// A successful build owns its signing state for its whole lifetime and
			// hands the host back unchanged: no keychain, no staged credential,
			// no installed profile and no run-owned xcconfig.
			for _, leaf := range []string{"certificate.p12", "pp.mobileprovision", "app-signing.keychain-db"} {
				_, leafErr := os.Lstat(fsys.Path("TMPDIR/" + leaf))
				require.ErrorIs(t, leafErr, os.ErrNotExist, "successful build must not leave "+leaf)
			}

			profiles, readErr := os.ReadDir(fsys.Path("HOME/Library/MobileDevice/Provisioning Profiles"))
			require.NoError(t, readErr, "the profiles directory itself is an ordinary user directory")
			require.Empty(t, profiles, "installed provisioning profile must be removed")

			if xcconfig != "" {
				_, statErr = os.Lstat(xcconfig)
				require.ErrorIs(t, statErr, os.ErrNotExist, "run-owned xcconfig must be removed")
			}

			for _, key := range []string{"CI_TEMP_DIR", "RUNNER_TEMP", "TEMP", "TMP"} {
				entries, readErr := os.ReadDir(fsys.Path(key))
				require.NoError(t, readErr)
				require.Empty(t, entries)
			}
		})
	}
}

func TestXcodeReleaseBuild_BoundedBlobsReachFirstSecurityEffect(t *testing.T) {
	for _, field := range []string{"xcconfig", "export", "certificate", "profile"} {
		for _, name := range []string{"multiline", "at_decoded_cap", "at_encoded_cap"} {
			t.Run(field+"/"+name, func(t *testing.T) {
				fsys, in := xcodeReleaseFixture(t)

				value := " \tY W\r\nJ j \n"
				if name == "at_decoded_cap" {
					value = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("x"), 16<<20))
				}

				if name == "at_encoded_cap" {
					value = "eA==" + strings.Repeat(" ", (32<<20)-4)
				}

				setXcodeBlob(t, &in, field, value)

				stop := errors.New("intentional first security effect") //nolint:err113 // per-test cause.
				sec, ops, sink := &fakeSecurity{err: stop}, &fakeXcodeBuild{}, fakeoutputsink.New(t)

				var stdout, stderr bytes.Buffer

				err := appbuild.XcodeReleaseBuild(t.Context(), sink, sec, ops, output.Annotator{}, &stdout, &stderr, in)
				require.ErrorIs(t, err, stop)
				require.ErrorContains(t, err, "set up code signing: create-keychain")
				require.Len(t, sec.calls, 1)
				require.Empty(t, ops.calls)
				require.Empty(t, sink.Keys())
				require.Equal(t, "IPA artifact: mobile-v2.0\n", stdout.String())
				require.Empty(t, stderr.String())

				_, statErr := os.Stat(fsys.Path("checkout/build"))
				require.ErrorIs(t, statErr, os.ErrNotExist)

				entries, readErr := os.ReadDir(fsys.Path("TMPDIR"))
				require.NoError(t, readErr)
				require.Empty(t, entries, "a refused setup leaves no staged signing material")
			})
		}
	}
}

func TestXcodeReleaseBuild_UnsignedIgnoresSigningAndExportButRequiresArchive(t *testing.T) {
	for _, produceArchive := range []bool{false, true} {
		fsys, in := xcodeReleaseFixture(t)
		in.EnableCodeSigning = false
		in.CertBase64, in.PPBase64, in.ExportOptionsBase64 = "%%%", "%%%", "%%%"
		in.CertPassphrase, in.KeychainPassword, in.XCConfigBase64 = "\x00", "\x00", " \t\n"

		fsys.WriteFile("checkout/build/export/old.ipa", []byte("unused export"))
		before := ownedTree(t, fsys.Path("checkout/build/export"))
		ops := &fakeXcodeBuild{run: func(args []string) {
			require.Equal(t, "archive", args[0])

			if produceArchive {
				fsys.MkdirAll("checkout/build/app.xcarchive")
			}
		}}
		sink := fakeoutputsink.New(t)

		var stdout, stderr bytes.Buffer

		err := appbuild.XcodeReleaseBuild(t.Context(), sink, unsignedXcodeSecurity{t}, ops, output.Annotator{}, &stdout, &stderr, in)
		require.Len(t, ops.calls, 1)
		require.Equal(t, before, ownedTree(t, fsys.Path("checkout/build/export")))

		if produceArchive {
			require.NoError(t, err)
			require.Equal(t, map[string]string{"ipa-name": "mobile-v2.0", "version": "2.0", "build": "200"}, sink.AllScalar())
			require.True(t, strings.HasSuffix(stdout.String(), "Built artifacts:\nbuild/app.xcarchive\n"))
		} else {
			require.ErrorIs(t, err, errs.ErrValidation)
			require.Empty(t, sink.Keys())
			require.NotContains(t, stdout.String(), "Built artifacts:")
		}
	}
}

func TestXcodeReleaseBuild_SignedBuildFailureStopsBeforePublication(t *testing.T) {
	for _, testCase := range []struct{ stage, failure string }{
		{"archive", "start_error"}, {"archive", "nonzero_exit"},
		{"-exportArchive", "start_error"}, {"-exportArchive", "nonzero_exit"},
	} {
		stage, failure := testCase.stage, testCase.failure
		t.Run(stage+"/"+failure, func(t *testing.T) {
			fsys, in := xcodeReleaseFixture(t)
			cause := errors.New("owned fake build failure") //nolint:err113 // distinct per-test port cause.
			ops := &fakeXcodeBuild{}

			var (
				events []string
				plist  string
			)

			sec := &xcodeJourneySecurity{observe: func(args []string) { events = append(events, args[0]) }}
			ops.run = func(args []string) {
				events = append(events, args[0])
				if args[0] == "-exportArchive" {
					plist = args[6]
					assertXcodeSecret(t, plist, "<plist/>")
				}

				if args[0] == stage {
					ops.exitCode = 17
					if failure == "start_error" {
						ops.err = errors.Join(cause, errs.ErrDependencyUnavailable)
					}
				} else {
					fsys.MkdirAll("checkout/build/app.xcarchive")
				}
			}
			sink := &xcodeJourneySink{Sink: fakeoutputsink.New(t), events: &events}

			var stdout, stderr bytes.Buffer

			err := appbuild.XcodeReleaseBuild(t.Context(), sink, sec, ops, output.Annotator{}, &stdout, &stderr, in)
			require.ErrorIs(t, err, errs.ErrDependencyUnavailable)

			if failure == "start_error" {
				require.ErrorIs(t, err, cause)
			} else {
				require.ErrorContains(t, err, "exited with status 17")
			}

			require.ErrorContains(t, err, "xcodebuild "+stage)

			wantEvents := []string{"create-keychain", "set-keychain-settings", "unlock-keychain", "import", "set-key-partition-list", "list-keychain", "list-keychain", "archive"}
			if stage == "-exportArchive" {
				wantEvents = append(wantEvents, stage)
			}
			// A build that never published still hands the host back unchanged.
			wantEvents = append(wantEvents, "delete-keychain")
			require.Equal(t, wantEvents, events)
			require.Empty(t, sink.Keys())
			require.NotContains(t, stdout.String(), "Built artifacts:")

			if plist != "" {
				_, statErr := os.Stat(plist)
				require.ErrorIs(t, statErr, os.ErrNotExist)
			}

			for _, leaf := range []string{"certificate.p12", "pp.mobileprovision", "app-signing.keychain-db"} {
				_, leafErr := os.Lstat(fsys.Path("TMPDIR/" + leaf))
				require.ErrorIs(t, leafErr, os.ErrNotExist, "a failed build must not leave "+leaf)
			}

			profiles, readErr := os.ReadDir(fsys.Path("HOME/Library/MobileDevice/Provisioning Profiles"))
			require.NoError(t, readErr)
			require.Empty(t, profiles, "installed provisioning profile must be removed")
		})
	}
}
