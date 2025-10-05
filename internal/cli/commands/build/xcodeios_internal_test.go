// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/ghaenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
	"github.com/stretchr/testify/require"
)

func TestXcodeIOSRun_ResolvesPasswordFilesOnlyWhenSigning(t *testing.T) {
	// The keychain password file is no longer read at all, so "keychain" here
	// asserts the opposite of what it used to: an unreadable one must not stop
	// a signed run, because the run mints its own password.
	for _, missing := range []string{"certificate", "keychain"} {
		for _, signed := range []bool{false, true} {
			t.Run(missing+"/signed_"+strconv.FormatBool(signed), func(t *testing.T) {
				outputs := ghaenv.Setup(t)
				fsys := testfs.NewReal(t)
				fsys.Chdir()

				for _, key := range []string{"HOME", "TMPDIR", "TEMP", "TMP"} {
					t.Setenv(key, fsys.MkdirAll(key))
				}
				// Even a future accidental tool lookup cannot reach host binaries.
				t.Setenv("PATH", fsys.MkdirAll("empty-bin"))
				fsys.MkdirAll("App.xcodeproj")
				cert := fsys.WriteFile("cert-password", []byte("synthetic cert password"))

				keychain := fsys.WriteFile("keychain-password", []byte("synthetic keychain password"))
				if missing == "certificate" {
					cert = fsys.Path("missing-cert-password")
				} else {
					keychain = fsys.Path("missing-keychain-password")
				}
				// All required CLI values are present. NUL reaches the real app's
				// archive preflight, before signing or xcodebuild can be invoked.
				err := xcodeIOSRunCmd(&xcodeCommandSecurity{}, &xcodeCommandBuild{}).Run(t.Context(), []string{
					"run", "--enable-code-signing=" + strconv.FormatBool(signed),
					"--artifact-name", "mobile", "--project", "App.xcodeproj",
					"--scheme", "App\x00", "--configuration", "Release", "--destination", "generic/platform=iOS",
					"--cert-base64", "Y2VydA==", "--pp-base64", "cHJvZmlsZQ==", "--export-options-base64", "PHBsaXN0Lz4=",
					"--cert-passphrase-file", cert, "--keychain-password-file", keychain,
				})
				if signed && missing == "certificate" {
					require.ErrorIs(t, err, errs.ErrMissingInput)
					require.ErrorContains(t, err, "read secret from")
				} else {
					require.ErrorIs(t, err, errs.ErrUsage)
					require.ErrorContains(t, err, "archive: xcode archive inputs must not contain NUL")
				}

				for _, path := range []string{outputs.OutputPath, outputs.SummaryPath} {
					body, readErr := os.ReadFile(path)
					require.NoError(t, readErr)
					require.Empty(t, body)
				}

				require.NoDirExists(t, fsys.Path("build"))

				for _, key := range []string{"HOME", "TMPDIR", "TEMP", "TMP"} {
					entries, readErr := os.ReadDir(fsys.Path(key))
					require.NoError(t, readErr)
					require.Empty(t, entries)
				}
			})
		}
	}
}

// xcodeCommandBuild records what the command asked xcodebuild to do and, when
// asked, materialises the archive the later steps look for.
type xcodeCommandBuild struct {
	calls [][]string
	run   func([]string)
}

func (f *xcodeCommandBuild) RunInherit(_ context.Context, _, _ io.Writer, args ...string) (int, error) {
	f.calls = append(f.calls, args)
	if f.run != nil {
		f.run(args)
	}

	return 0, nil
}

// xcodeCommandSecurity records the macOS security-CLI invocations. failWith,
// when set, is returned from the first call.
type xcodeCommandSecurity struct {
	calls    [][]string
	failWith error
}

func (s *xcodeCommandSecurity) Run(_ context.Context, args ...string) (string, error) {
	s.calls = append(s.calls, args)

	if s.failWith != nil {
		return "", s.failWith
	}

	return "", nil
}

// TestXcodeIOSRun_CLIInputsReachXcodebuild is the root-level counterpart of the
// Android command's oracle.
//
// The other test in this file drives the same command, but every case it covers
// is engineered to refuse before a tool runs — a NUL in --scheme trips the
// archive preflight. So nothing here had ever asserted the successful path:
// which flags and environment variables the command reads, which of the two
// wins when both are set, and what argv that composes for xcodebuild. The app
// layer pins the argv given an input struct; nobody pinned that the CLI builds
// the struct the operator asked for.
//
// That gap is exactly where a mobile release goes wrong quietly. Reading
// CONFIGURATION when the flag said Release, or dropping --build-number, still
// produces a signed, published IPA — of the wrong thing.
func TestXcodeIOSRun_CLIInputsReachXcodebuild(t *testing.T) {
	outputs := ghaenv.Setup(t)
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	fsys.WriteFile("App.xcodeproj/project.pbxproj",
		[]byte("MARKETING_VERSION = 3.4.5;\nCURRENT_PROJECT_VERSION = 77;\n"))

	// Decoys: a second project and a conflicting environment value, so
	// "selected the one the flag named" is distinguishable from "found the
	// only candidate".
	fsys.WriteFile("Decoy.xcodeproj/project.pbxproj",
		[]byte("MARKETING_VERSION = 9.9.9;\nCURRENT_PROJECT_VERSION = 999;\n"))
	t.Setenv("CONFIGURATION", "Debug")
	t.Setenv("DESTINATION", "generic/platform=iOS")
	t.Setenv("REPOSITORY_NAME", "mobile")

	builder := &xcodeCommandBuild{run: func([]string) {
		fsys.WriteFile("build/app.xcarchive/Info.plist", []byte("owned fake archive"))
	}}
	sec := &xcodeCommandSecurity{}

	err := xcodeIOSRunCmd(sec, builder).Run(t.Context(), []string{
		"run", "--enable-code-signing=false",
		"--artifact-name", "mobile", "--project", "App.xcodeproj",
		"--scheme", "App",
		// Set on the flag AND in the environment, with different values.
		"--configuration", "Release",
		"--build-number", "888",
	})
	require.NoError(t, err)

	// Unsigned means the security CLI is never reached: no keychain is
	// created, so nothing has to be cleaned up.
	require.Empty(t, sec.calls, "an unsigned run invoked the macOS security CLI")

	require.Len(t, builder.calls, 1, "xcodebuild calls = %q", builder.calls)
	require.Equal(t, []string{
		"archive", "-project", "App.xcodeproj", "-scheme", "App",
		"-configuration", "Release", // the flag, not $CONFIGURATION=Debug
		"-archivePath", "build/app.xcarchive",
		"-destination", "generic/platform=iOS", // taken from the environment
		"-skipPackagePluginValidation", "CURRENT_PROJECT_VERSION=888",
	}, builder.calls[0])

	// The metadata published to the forge comes from the selected project,
	// not the decoy, and the build override does not replace it.
	body, readErr := os.ReadFile(outputs.OutputPath)
	require.NoError(t, readErr)
	require.Contains(t, string(body), "version=3.4.5")
	require.Contains(t, string(body), "build=77")
	require.NotContains(t, string(body), "9.9.9")
}

// TestXcodeIOSRun_SigningInputsReachTheSecurityCLI covers the other port.
//
// The signing material arrives as CLI values and environment variables and has
// to reach the macOS keychain unchanged. Nothing at this layer checked that it
// reached the security CLI at all: with no seam, a signed run could only be
// tested by making it fail first.
func TestXcodeIOSRun_SigningInputsReachTheSecurityCLI(t *testing.T) {
	ghaenv.Setup(t)

	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile("App.xcodeproj/project.pbxproj",
		[]byte("MARKETING_VERSION = 1.0;\nCURRENT_PROJECT_VERSION = 1;\n"))
	t.Setenv("TMPDIR", fsys.MkdirAll("tmp"))

	builder := &xcodeCommandBuild{}
	sec := &xcodeCommandSecurity{failWith: errSigningRefused}

	err := xcodeIOSRunCmd(sec, builder).Run(t.Context(), []string{
		"run", "--enable-code-signing=true",
		"--artifact-name", "mobile", "--project", "App.xcodeproj",
		"--scheme", "App", "--configuration", "Release", "--destination", "generic/platform=iOS",
		"--cert-base64", "Y2VydA==", "--pp-base64", "cHJvZmlsZQ==",
		"--export-options-base64", "PHBsaXN0Lz4=",
		"--cert-passphrase-file", fsys.WriteFile("cert-password", []byte("synthetic cert password")),
	})
	require.Error(t, err, "the security CLI refused; the command must not report success")

	require.NotEmpty(t, sec.calls, "signing was enabled but the security CLI was never invoked")
	require.Empty(t, builder.calls, "xcodebuild ran even though signing setup failed")

	// A refused signing setup must not leave the certificate on disk.
	entries, readErr := os.ReadDir(fsys.Path("tmp"))
	require.NoError(t, readErr)
	require.Empty(t, entries, "signing material was left behind after the refusal")
}

// errSigningRefused stands in for the macOS security CLI rejecting the import.
var errSigningRefused = errors.New("security: import refused") //nolint:err113 // test fixture sentinel.
