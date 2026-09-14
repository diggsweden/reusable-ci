// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/stretchr/testify/require"
)

// quotedKeychainList renders paths the way security(1) prints a search list.
func quotedKeychainList(paths ...string) string {
	var out strings.Builder
	for _, path := range paths {
		out.WriteString("    \"" + path + "\"\n")
	}

	return out.String()
}

// TestXcodeSigningLifecycle_PreservesAndRestoresTheSearchList pins the whole
// point of reading the list first: `-s` replaces it, so a run that named only
// its own keychain would drop the host's keychains for the rest of the session
// and leave nothing to put back. The list read out of security(1) is tool text
// that goes straight into the next argv, so only quoted absolute control-free
// paths may come back, and a bounded number of them.
func TestXcodeSigningLifecycle_PreservesAndRestoresTheSearchList(t *testing.T) {
	login := "/Users/runner/Library/Keychains/login.keychain-db"
	system := "/Library/Keychains/System.keychain"

	for _, testCase := range []struct {
		name  string
		list  func(keychain string) string
		prior func(keychain string) []string
	}{
		{
			name:  "ordinary user list",
			list:  func(string) string { return quotedKeychainList(login, system) },
			prior: func(string) []string { return []string{login, system} },
		},
		{
			name: "unquoted, relative and control-bearing lines are not keychains",
			list: func(string) string {
				return "Keychains for the user domain:\n" + quotedKeychainList(login) +
					quotedKeychainList("relative/login.keychain-db") +
					"    \"/Users/runner/bad\nname.keychain-db\"\n" + "   \n"
			},
			prior: func(string) []string { return []string{login} },
		},
		{
			name:  "a keychain already listed is not listed twice",
			list:  func(keychain string) string { return quotedKeychainList(login, keychain) },
			prior: func(keychain string) []string { return []string{login, keychain} },
		},
		{
			name:  "nothing readable leaves nothing to restore",
			list:  func(string) string { return "security: no keychains\n" },
			prior: func(string) []string { return nil },
		},
		{
			name:  "an unbounded list is capped",
			list:  func(string) string { return quotedKeychainList(manyKeychains(65)...) },
			prior: func(string) []string { return manyKeychains(64) },
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			in := xcodeSecurityFixture(t)
			keychain := filepath.Join(in.TempDir, "app-signing.keychain-db")
			sec, out := &recordingXcodeSecurity{}, &xcodeSecurityWriter{}
			sec.run = func(index int) (string, error) {
				if sec.calls[index][0] == "list-keychain" && len(sec.calls[index]) == 3 {
					return testCase.list(keychain), nil
				}

				return "", nil
			}
			require.NoError(t, appbuild.XcodeSetupCodeSigning(t.Context(), sec, out, in))

			prior := testCase.prior(keychain)

			search := prior
			if !slices.Contains(prior, keychain) {
				search = append(slices.Clone(prior), keychain)
			}

			require.Equal(t, append([]string{"list-keychain", "-d", "user", "-s"}, search...), sec.calls[6],
				"this run is appended to the host's own keychains, never substituted for them")

			// The same state, restored by the owning build rather than the
			// standalone verb, must put back exactly what was read.
			state := teardownAfterRelease(t, in, testCase.list)
			if len(prior) == 0 {
				require.Equal(t, [][]string{{"delete-keychain", state.keychain}}, state.teardown,
					"an empty read is not restored: `-s` with no names would clear the list")

				return
			}

			require.Equal(t, [][]string{
				append([]string{"list-keychain", "-d", "user", "-s"}, testCase.prior(state.keychain)...),
				{"delete-keychain", state.keychain},
			}, state.teardown)
		})
	}
}

func manyKeychains(count int) []string {
	list := make([]string, 0, count)
	for index := range count {
		list = append(list, "/Library/Keychains/owned-"+strconv.Itoa(index)+".keychain-db")
	}

	return list
}

type xcodeTeardownRecord struct {
	keychain string
	teardown [][]string
}

// teardownAfterRelease runs one signed release build and returns the security
// calls its teardown made, separated from the calls that installed the state.
func teardownAfterRelease(t *testing.T, in appbuild.XcodeSetupCodeSigningInput, list func(string) string) xcodeTeardownRecord {
	t.Helper()
	fsys, release := xcodeReleaseFixture(t)
	release.CertBase64, release.PPBase64 = in.CertBase64, in.PPBase64
	release.CertPassphrase, release.KeychainPassword = in.CertPassphrase, in.KeychainPassword
	keychain := fsys.Path("TMPDIR/app-signing.keychain-db")
	sec := &recordingXcodeSecurity{}
	installed := 0
	sec.run = func(index int) (string, error) {
		if sec.calls[index][0] == "list-keychain" && len(sec.calls[index]) == 3 {
			return list(keychain), nil
		}

		return "", nil
	}
	ops := &fakeXcodeBuild{run: func(args []string) {
		if installed == 0 {
			installed = len(sec.calls)
		}

		if args[0] == "archive" {
			fsys.WriteFile("checkout/build/app.xcarchive/Info.plist", []byte("owned synthetic archive"))

			return
		}

		fsys.WriteFile("checkout/build/export/App.ipa", []byte("owned synthetic IPA"))
	}}

	var stdout, stderr bytes.Buffer
	require.NoError(t, appbuild.XcodeReleaseBuild(t.Context(), fakeoutputsink.New(t), sec, ops, output.Annotator{}, &stdout, &stderr, release))
	require.Positive(t, installed)

	return xcodeTeardownRecord{keychain: keychain, teardown: sec.calls[installed:]}
}

// TestXcodeSigningLifecycle_DestinationAdversaries covers the destinations a
// signing setup writes to. Every unusable one is refused before a single byte,
// keychain or security(1) call exists, and the blocking canary survives
// untouched: a link is never followed, a non-file is never replaced, and an
// existing keychain is never adopted by a run that would later delete it.
func TestXcodeSigningLifecycle_DestinationAdversaries(t *testing.T) { //nolint:gocognit // one destination policy, one fixture shape, many adversaries.
	for _, testCase := range []struct {
		name    string
		arrange func(t *testing.T, in *appbuild.XcodeSetupCodeSigningInput) string
		cause   error
		message string
	}{
		{
			name: "staged certificate is a symlink",
			arrange: func(t *testing.T, in *appbuild.XcodeSetupCodeSigningInput) string {
				t.Helper()

				target := filepath.Join(filepath.Dir(in.TempDir), "elsewhere.p12")
				require.NoError(t, os.WriteFile(target, []byte("unrelated caller file"), 0o600))
				require.NoError(t, os.Symlink(target, filepath.Join(in.TempDir, "certificate.p12")))

				return target
			},
			cause: errs.ErrValidation, message: "must be a regular file",
		},
		{
			name: "staged profile is a directory",
			arrange: func(t *testing.T, in *appbuild.XcodeSetupCodeSigningInput) string {
				t.Helper()
				require.NoError(t, os.Mkdir(filepath.Join(in.TempDir, "pp.mobileprovision"), 0o700))

				return ""
			},
			cause: errs.ErrValidation, message: "must be a regular file",
		},
		{
			name: "a keychain of that name already exists",
			arrange: func(t *testing.T, in *appbuild.XcodeSetupCodeSigningInput) string {
				t.Helper()

				path := filepath.Join(in.TempDir, "app-signing.keychain-db")
				require.NoError(t, os.WriteFile(path, []byte("someone else's keychain"), 0o600))

				return path
			},
			cause: errs.ErrValidation, message: "must not already exist",
		},
		{
			name: "the keychain name is a link to a caller file",
			arrange: func(t *testing.T, in *appbuild.XcodeSetupCodeSigningInput) string {
				t.Helper()

				target := filepath.Join(filepath.Dir(in.TempDir), "caller.keychain-db")
				require.NoError(t, os.WriteFile(target, []byte("unrelated caller keychain"), 0o600))
				require.NoError(t, os.Symlink(target, filepath.Join(in.TempDir, "app-signing.keychain-db")))

				return target
			},
			cause: errs.ErrValidation, message: "must not already exist",
		},
		{
			name: "the staging directory is reached through a link",
			arrange: func(t *testing.T, in *appbuild.XcodeSetupCodeSigningInput) string {
				t.Helper()

				alias := filepath.Join(filepath.Dir(in.TempDir), "aliased scratch")
				require.NoError(t, os.Symlink(in.TempDir, alias))
				in.TempDir = alias

				return ""
			},
			cause: errs.ErrValidation, message: "open signing staging directory",
		},
		{
			name: "profiles and staging are the same directory",
			arrange: func(t *testing.T, in *appbuild.XcodeSetupCodeSigningInput) string {
				t.Helper()

				in.ProvisioningProfilesDir = in.TempDir

				return ""
			},
			cause: errs.ErrValidation, message: "must differ from the signing staging dir",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			in := xcodeSecurityFixture(t)
			canary := testCase.arrange(t, &in)
			before := ownedTree(t, filepath.Dir(in.TempDir))
			sec, out := &recordingXcodeSecurity{}, &xcodeSecurityWriter{}
			err := appbuild.XcodeSetupCodeSigning(t.Context(), sec, out, in)
			require.ErrorIs(t, err, testCase.cause)
			require.ErrorContains(t, err, testCase.message)
			require.Empty(t, sec.calls, "an unusable destination is refused before any security call")
			require.Empty(t, out.calls)
			require.Equal(t, before, ownedTree(t, filepath.Dir(in.TempDir)), "a refused setup changes nothing at all")

			if canary != "" {
				body, readErr := os.ReadFile(canary)
				require.NoError(t, readErr)
				require.NotContains(t, string(body), xcodeContractCertificate)
				require.NotContains(t, string(body), xcodeContractProfile)
			}

			assertXcodeSecurityNonleakage(t, in, err.Error(), out.accepted.String())
		})
	}
}

// TestXcodeSigningLifecycle_ReplacesOwnStaleMaterial keeps the ordinary rerun
// working: leftover regular files from an earlier run are replaced in place,
// owner-only, and a broader mode does not survive the replacement.
func TestXcodeSigningLifecycle_ReplacesOwnStaleMaterial(t *testing.T) {
	in := xcodeSecurityFixture(t)
	require.NoError(t, os.MkdirAll(in.ProvisioningProfilesDir, 0o700))

	for _, path := range []string{
		filepath.Join(in.TempDir, "certificate.p12"),
		filepath.Join(in.TempDir, "pp.mobileprovision"),
		filepath.Join(in.ProvisioningProfilesDir, "pp.mobileprovision"),
	} {
		require.NoError(t, os.WriteFile(path, []byte("stale world-readable material"), 0o644)) //nolint:gosec // deliberate broad canary mode.
	}

	sec, out := &recordingXcodeSecurity{}, &xcodeSecurityWriter{}
	require.NoError(t, appbuild.XcodeSetupCodeSigning(t.Context(), sec, out, in))
	assertXcodeSecret(t, filepath.Join(in.TempDir, "certificate.p12"), xcodeContractCertificate)
	assertXcodeSecret(t, filepath.Join(in.TempDir, "pp.mobileprovision"), xcodeContractProfile)
	assertXcodeSecret(t, filepath.Join(in.ProvisioningProfilesDir, "pp.mobileprovision"), xcodeContractProfile)
}

// TestXcodeSigningLifecycle_TeardownFailuresAreReportedNotMasked pins what a
// caller learns when the host cannot be restored. Credentials left installed
// are a result worth failing on, and a teardown fault must never replace the
// reason the build stopped in the first place.
func TestXcodeSigningLifecycle_TeardownFailuresAreReportedNotMasked(t *testing.T) {
	for _, testCase := range []string{"successful build", "failed build", "failed setup"} {
		t.Run(testCase, func(t *testing.T) {
			in := xcodeSecurityFixture(t)
			fsys, release := xcodeReleaseFixture(t)
			release.CertBase64, release.PPBase64 = in.CertBase64, in.PPBase64
			release.CertPassphrase, release.KeychainPassword = in.CertPassphrase, in.KeychainPassword
			teardownCause := errors.New("owned teardown refusal") //nolint:err113 // distinct port cause.
			primary := errors.New("owned primary refusal")        //nolint:err113 // distinct port cause.
			sec := &recordingXcodeSecurity{}
			sec.run = func(index int) (string, error) {
				switch {
				case sec.calls[index][0] == "delete-keychain":
					return "", teardownCause
				case testCase == "failed setup" && index == 3:
					return "", primary
				default:
					return "", nil
				}
			}
			ops := &fakeXcodeBuild{}
			ops.run = func(args []string) {
				if testCase == "failed build" {
					ops.err = primary

					return
				}

				if args[0] == "archive" {
					fsys.WriteFile("checkout/build/app.xcarchive/Info.plist", []byte("owned synthetic archive"))

					return
				}

				fsys.WriteFile("checkout/build/export/App.ipa", []byte("owned synthetic IPA"))
			}
			sink := fakeoutputsink.New(t)

			var stdout, stderr bytes.Buffer

			err := appbuild.XcodeReleaseBuild(t.Context(), sink, sec, ops, output.Annotator{}, &stdout, &stderr, release)
			require.ErrorIs(t, err, teardownCause, "a host left holding credentials is a reported failure")
			require.ErrorContains(t, err, "delete-keychain: security command failed")

			if testCase == "successful build" {
				// The artifacts were produced and published; only the restoration failed.
				require.Equal(t, map[string]string{"ipa-name": "mobile-v2.0", "version": "2.0", "build": "200"}, sink.AllScalar())
				require.Contains(t, stdout.String(), "Built artifacts:")

				return
			}

			require.ErrorIs(t, err, primary, "the primary cause survives a failing teardown")
			require.Empty(t, sink.Keys())
			require.True(t, strings.HasPrefix(err.Error(), primaryPrefix(testCase)), err.Error())
			assertXcodeSecurityNonleakage(t, in, err.Error(), fmt.Sprintf("%v\n%+v", err, err), stdout.String(), stderr.String())
		})
	}
}

func primaryPrefix(testCase string) string {
	if testCase == "failed setup" {
		return "set up code signing: security import"
	}

	return "archive: xcodebuild archive"
}

// TestXcodeSigningLifecycle_KeychainPasswordIsMintedPerRun covers the decision
// behind D246.
//
// security(1) takes keychain and import passwords only as argv, so whatever
// value is used is visible to a process listing on the build host. That cannot
// be removed at the call. What can be removed is the reason it matters: the
// keychain is created by this run, holds only the certificate it just imported,
// and is deleted when the build ends, so nothing needs the operator to supply a
// password for it. The run mints its own, and an operator secret that used to
// be required now never reaches an argv at all.
//
// A supplied value is accepted and ignored rather than refused, so callers that
// still pass KEYCHAIN_PASSWORD keep working while the input is retired.
func TestXcodeSigningLifecycle_KeychainPasswordIsMintedPerRun(t *testing.T) { //nolint:paralleltest // the shared fixture sets process environment.
	passwords := make([]string, 0, 2)

	for run := range 2 {
		in := xcodeSecurityFixture(t)
		in.KeychainPassword = "operator-supplied-secret-" + strconv.Itoa(run)

		sec, out := &recordingXcodeSecurity{}, &xcodeSecurityWriter{}
		require.NoError(t, appbuild.XcodeSetupCodeSigning(t.Context(), sec, out, in))

		generated := observedKeychainPassword(t, sec.calls)
		requireGeneratedKeychainPassword(t, in.KeychainPassword, generated)

		// Every call that needs the password uses the same one: create, unlock
		// and set-key-partition-list are one keychain, not three.
		require.Equal(t, generated, sec.calls[2][2], "unlock-keychain")
		require.Equal(t, generated, sec.calls[4][4], "set-key-partition-list")

		for _, call := range sec.calls {
			require.NotContains(t, strings.Join(call, " "), in.KeychainPassword,
				"the supplied secret must not appear in any security argv")
		}

		assertXcodeSecurityNonleakage(t, in, out.accepted.String())

		passwords = append(passwords, generated)
	}

	require.NotEqual(t, passwords[0], passwords[1], "each run mints its own password")
}

// TestXcodeSigningLifecycle_NoKeychainPasswordIsRequired proves the input is
// retired rather than merely defaulted: signing works with none supplied.
func TestXcodeSigningLifecycle_NoKeychainPasswordIsRequired(t *testing.T) { //nolint:paralleltest // the shared fixture sets process environment.
	in := xcodeSecurityFixture(t)
	in.KeychainPassword = ""

	sec, out := &recordingXcodeSecurity{}, &xcodeSecurityWriter{}
	require.NoError(t, appbuild.XcodeSetupCodeSigning(t.Context(), sec, out, in))
	require.NotEmpty(t, observedKeychainPassword(t, sec.calls))
}
