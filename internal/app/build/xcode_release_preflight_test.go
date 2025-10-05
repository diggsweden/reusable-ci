// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"encoding/base64"
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

func xcodeReleaseFixture(t *testing.T) (*testfs.Real, appbuild.XcodeReleaseBuildInput) {
	t.Helper()
	fsys := testfs.NewReal(t)

	var err error

	fsys.Root, err = filepath.EvalSymlinks(fsys.Root)
	require.NoError(t, err)
	// Every default used by the direct app journey stays in the owned tree.
	for _, key := range []string{"HOME", "TMPDIR", "TEMP", "TMP", "CI_TEMP_DIR", "RUNNER_TEMP"} {
		t.Setenv(key, fsys.MkdirAll(key))
	}

	t.Chdir(fsys.MkdirAll("checkout"))
	makeXcodeProject(t, fsys, "checkout/Alpha", "1.0", "100")
	makeXcodeProject(t, fsys, "checkout/Sources/Zebra", "2.0", "200")
	fsys.WriteFile("checkout/App.xcworkspace/contents.xcworkspacedata", []byte(`<Workspace><FileRef location="group:Sources/Zebra.xcodeproj"/></Workspace>`))

	return fsys, appbuild.XcodeReleaseBuildInput{
		ArtifactName: "mobile", RepositoryName: "ignored", IncludeTag: true, RefName: "v2.0",
		Workspace: "App.xcworkspace", Scheme: "Zebra", Configuration: "Release", Destination: "generic/platform=iOS", BuildNumber: "999",
		EnableCodeSigning: true, CertBase64: "Y2VydA==", PPBase64: "cHJvZmlsZQ==",
		CertPassphrase: "synthetic cert password", KeychainPassword: "synthetic keychain password",
		XCConfigBase64: "U0VUVElORyA9IHZhbHVlCg==", ExportOptionsBase64: "PHBsaXN0Lz4=",
	}
}

func TestXcodeReleaseBuild_PreflightRefusalsLeaveAllChannelsUntouched(t *testing.T) {
	tests := []struct {
		name        string
		change      func(*appbuild.XcodeReleaseBuildInput)
		wantErr     error
		errContains string
	}{
		{"name", func(in *appbuild.XcodeReleaseBuildInput) { in.ArtifactName, in.RepositoryName = "", "" }, errs.ErrUsage, "compose artifact name"},
		{"scheme", func(in *appbuild.XcodeReleaseBuildInput) { in.Scheme = "" }, errs.ErrUsage, "archive: scheme"},
		{"configuration", func(in *appbuild.XcodeReleaseBuildInput) { in.Configuration = "" }, errs.ErrUsage, "archive: configuration"},
		{"destination", func(in *appbuild.XcodeReleaseBuildInput) { in.Destination = "" }, errs.ErrUsage, "archive: destination"},
		{"no_selector", func(in *appbuild.XcodeReleaseBuildInput) { in.Workspace = "" }, errs.ErrUsage, "exactly one"},
		{"both_selectors", func(in *appbuild.XcodeReleaseBuildInput) { in.Project = "Sources/Zebra.xcodeproj" }, errs.ErrUsage, "exactly one"},
		{"noncanonical_selector", func(in *appbuild.XcodeReleaseBuildInput) { in.Workspace += "/." }, errs.ErrUsage, "archive: xcode identity"},
		{"NUL_workspace", func(in *appbuild.XcodeReleaseBuildInput) { in.Workspace = "App\x00.xcworkspace" }, errs.ErrUsage, "NUL"},
		{"NUL_project", func(in *appbuild.XcodeReleaseBuildInput) { in.Workspace, in.Project = "", "App\x00.xcodeproj" }, errs.ErrUsage, "NUL"},
		{"NUL_scheme", func(in *appbuild.XcodeReleaseBuildInput) { in.Scheme += "\x00" }, errs.ErrUsage, "NUL"},
		{"NUL_configuration", func(in *appbuild.XcodeReleaseBuildInput) { in.Configuration += "\x00" }, errs.ErrUsage, "NUL"},
		{"NUL_destination", func(in *appbuild.XcodeReleaseBuildInput) { in.Destination += "\x00" }, errs.ErrUsage, "NUL"},
		{"NUL_build_number", func(in *appbuild.XcodeReleaseBuildInput) { in.BuildNumber += "\x00" }, errs.ErrUsage, "NUL"},
		{"NUL_cert_password", func(in *appbuild.XcodeReleaseBuildInput) { in.CertPassphrase += "\x00" }, errs.ErrUsage, "NUL"},
		{"NUL_keychain_password", func(in *appbuild.XcodeReleaseBuildInput) { in.KeychainPassword += "\x00" }, errs.ErrUsage, "NUL"},
	}
	for _, testCase := range tests {
		for _, seeded := range []bool{false, true} {
			t.Run(testCase.name+map[bool]string{false: "/fresh", true: "/seeded"}[seeded], func(t *testing.T) {
				fsys, in := xcodeReleaseFixture(t)
				if seeded {
					seedXcodeCanaries(t, fsys)
				}

				testCase.change(&in)
				assertXcodePreflightRefusal(t, fsys, in, testCase.wantErr, testCase.errContains)
			})
		}
	}
}

func seedXcodeCanaries(t *testing.T, fsys *testfs.Real) {
	t.Helper()

	for _, path := range []string{"TMPDIR/certificate.p12", "TMPDIR/pp.mobileprovision", "TMPDIR/app-signing.keychain-db", "TMPDIR/ci-canary.xcconfig", "HOME/Library/MobileDevice/Provisioning Profiles/pp.mobileprovision", "checkout/export-options.plist", "checkout/build/unrelated/old.ipa"} {
		fsys.WriteFile(path, []byte("preserve "+path))
		require.NoError(t, os.Chmod(fsys.Path(path), 0o640)) //nolint:gosec // owned canary deliberately has a mode distinct from newly staged secrets.
	}
}

func assertXcodePreflightRefusal(t *testing.T, fsys *testfs.Real, in appbuild.XcodeReleaseBuildInput, wantErr error, errContains string) {
	t.Helper()
	before, beforeEnv := ownedTree(t, fsys.Root), os.Environ()
	sink := fakeoutputsink.New(t)
	sec, ops := &fakeSecurity{}, &fakeXcodeBuild{}

	var stdout, stderr, annotations bytes.Buffer

	err := appbuild.XcodeReleaseBuild(t.Context(), sink, sec, ops, output.NewAnnotator(&annotations, output.FormatGitHub), &stdout, &stderr, in)
	// Check preservation even when the returned cause is also wrong.
	if stdout.Len() != 0 || stderr.Len() != 0 || annotations.Len() != 0 || len(sec.calls) != 0 || len(ops.calls) != 0 || len(sink.Keys()) != 0 || sink.CloseCount() != 0 {
		t.Errorf("preflight effects: stdout=%q stderr=%q annotations=%q security=%d build=%d outputs=%v", &stdout, &stderr, &annotations, len(sec.calls), len(ops.calls), sink.Keys())
	}

	require.True(t, maps.Equal(before, ownedTree(t, fsys.Root)), "preflight changed paths, bytes, modes or links")
	require.Equal(t, beforeEnv, os.Environ())
	require.ErrorIs(t, err, wantErr)
	require.ErrorContains(t, err, errContains)
}

func TestXcodeReleaseBuild_PreflightValidatesEverySelectedBlob(t *testing.T) {
	for _, field := range []string{"xcconfig", "export", "certificate", "profile"} {
		for _, invalid := range []string{"malformed", "missing", "blank", "decoded_over_cap", "encoded_over_cap"} {
			if field == "xcconfig" && (invalid == "blank" || invalid == "missing") {
				continue // Optional xcconfig retains the blank no-op policy.
			}

			t.Run(field+"/"+invalid, func(t *testing.T) {
				value := "%%%"
				wantErr, cause := errs.ErrMalformedInput, "decode"

				switch invalid {
				case "missing":
					value = ""

					wantErr, cause = errs.ErrPermissionDenied, "secret not found"
					if field == "export" {
						wantErr, cause = errs.ErrMissingInput, "EXPORT_OPTIONS_BASE64"
					}
				case "blank":
					value = " \t\r\n"
				case "decoded_over_cap":
					value = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("x"), (16<<20)+1))
				case "encoded_over_cap":
					value = "eA==" + strings.Repeat(" ", 32<<20)
				}

				for _, destination := range []string{"fresh", "seeded"} {
					t.Run(destination, func(t *testing.T) {
						fsys, in := xcodeReleaseFixture(t)
						if destination == "seeded" {
							seedXcodeCanaries(t, fsys)
						}

						setXcodeBlob(t, &in, field, value)
						assertXcodePreflightRefusal(t, fsys, in, wantErr, cause)
					})
				}
			})
		}
	}
}

func setXcodeBlob(t *testing.T, in *appbuild.XcodeReleaseBuildInput, field, value string) {
	t.Helper()

	switch field {
	case "xcconfig":
		in.XCConfigBase64 = value
	case "export":
		in.ExportOptionsBase64 = value
	case "certificate":
		in.CertBase64 = value
	case "profile":
		in.PPBase64 = value
	default:
		t.Fatalf("unknown blob %q", field)
	}
}

func TestXcodeReleaseBuild_PreflightMetadataRefusalIsSilent(t *testing.T) {
	for _, descriptor := range []string{"<Workspace/>", `<Workspace><FileRef location="group:Alpha.xcodeproj"/><FileRef location="group:Sources/Zebra.xcodeproj"/></Workspace>`} {
		fsys, in := xcodeReleaseFixture(t)
		seedXcodeCanaries(t, fsys)
		fsys.WriteFile("checkout/App.xcworkspace/contents.xcworkspacedata", []byte(descriptor))
		assertXcodePreflightRefusal(t, fsys, in, errs.ErrValidation, "resolve version")
	}
}

func TestXcodeReleaseBuild_LateInvalidBlobDoesNotReportMissingPBX(t *testing.T) {
	fsys, in := xcodeReleaseFixture(t)
	in.Project, in.Workspace, in.ExportOptionsBase64 = "Sources/Zebra.xcodeproj", "", "%%%"

	require.NoError(t, os.Remove(fsys.Path("checkout/Sources/Zebra.xcodeproj/project.pbxproj")))
	assertXcodePreflightRefusal(t, fsys, in, errs.ErrMalformedInput, "export IPA: decode export options")
}
