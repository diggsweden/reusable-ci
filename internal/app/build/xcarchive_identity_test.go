// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	domainbuild "github.com/diggsweden/reusable-ci/v3/internal/domain/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/stretchr/testify/require"
)

// xcarchiveInfoPlist renders the part of an .xcarchive Info.plist the release
// flow reads. Shared with the security-contract fixture so both exercise the
// same document shape.
func xcarchiveInfoPlist(scheme string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>ArchiveVersion</key>
	<string>2</string>
	<key>CreationDate</key>
	<string>2026-09-10T12:00:00Z</string>
	<key>Name</key>
	<string>App</string>
	<key>SchemeName</key>
	<string>` + scheme + `</string>
</dict>
</plist>
`
}

// The release flow selects a scheme, passes it to xcodebuild, and publishes
// what appears at the archive path. The argv is pinned exactly and a
// pre-existing archive is refused, so the bundle provably came from this run's
// invocation — but what xcodebuild actually BUILT was never read back. An
// archive for a different scheme would be signed, exported and published as the
// release with nothing to show for it.
//
// The reader is deliberately narrow about when it can answer, because only a
// disagreement is evidence of a problem. An absent, unreadable, or
// SchemeName-less plist says nothing about which scheme was used.
func TestArchiveSchemeName_ReadsOnlyWhatTheDocumentActuallySays(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		body    string
		want    string
		wantErr bool
		why     string
	}{
		{
			name: "an ordinary archive plist", body: xcarchiveInfoPlist("Zebra"), want: "Zebra",
			why: "the shape xcodebuild writes",
		},
		{
			name: "a scheme with spaces", body: xcarchiveInfoPlist("My App Release"), want: "My App Release",
			why: "scheme names are display strings, not identifiers",
		},
		{
			name: "surrounding whitespace is trimmed",
			body: `<plist><dict><key>SchemeName</key><string>  Zebra  </string></dict></plist>`,
			want: "Zebra", why: "leading or trailing space would fail an otherwise correct comparison",
		},
		{
			name: "SchemeName is not the first key",
			body: `<plist><dict><key>Name</key><string>App</string><key>SchemeName</key><string>Zebra</string></dict></plist>`,
			want: "Zebra", why: "key order is not fixed, and pairing by index has to survive that",
		},
		{
			name: "no SchemeName key at all",
			body: `<plist><dict><key>Name</key><string>App</string></dict></plist>`,
			want: "", why: "reported as unverified rather than refused; it is not evidence of a wrong scheme",
		},
		{
			name: "a binary plist", body: "bplist00\x00\x01\x02", wantErr: true,
			why: "the encoding cannot be read here; that is a limit of the reader, not a wrong archive",
		},
		{
			name: "not a plist at all", body: "owned synthetic archive", wantErr: true,
			why: "the same: unreadable is not evidence",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := domainbuild.ArchiveSchemeName([]byte(tc.body))

			if tc.wantErr {
				require.Errorf(t, err, "an unreadable document was read as %q: %s", got, tc.why)

				return
			}

			require.NoErrorf(t, err, "%s", tc.why)
			require.Equalf(t, tc.want, got, "%s", tc.why)
		})
	}
}

// The end of the chain: a bundle recording a different scheme must not be
// published. This is the case the whole reader exists for — an archive for
// another target, signed and exported as this release.
// No t.Parallel: xcodeReleaseFixture uses t.Setenv and t.Chdir.
func TestXcodeReleaseBuild_RefusesAnArchiveBuiltForAnotherScheme(t *testing.T) {
	for _, tc := range []struct {
		name     string
		recorded string
		wantErr  bool
		wantNote string
		why      string
	}{
		{
			name: "the archive agrees", recorded: "Zebra",
			wantNote: "Archive scheme verified: Zebra",
			why:      "the ordinary case; without it the refusal below could be any failure",
		},
		{
			name: "the archive was built for something else", recorded: "Giraffe", wantErr: true,
			why: "an archive for another target would otherwise be signed and published as this release",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys, in := xcodeReleaseFixture(t)

			// Unsigned: the archive-identity check runs before export, and
			// signing would drag the keychain lifecycle into a test about
			// which scheme the bundle records.
			in.EnableCodeSigning = false
			in.CertBase64, in.PPBase64 = "", ""
			in.CertPassphrase, in.KeychainPassword = "", ""
			in.Scheme = "Zebra"

			buildOps := &fakeXcodeBuild{run: func(args []string) {
				if len(args) > 0 && args[0] == "archive" {
					fsys.WriteFile("checkout/build/app.xcarchive/Info.plist", []byte(xcarchiveInfoPlist(tc.recorded)))
				}
			}}

			var out, stderr strings.Builder

			err := appbuild.XcodeReleaseBuild(t.Context(), fakeoutputsink.New(t), unsignedXcodeSecurity{t}, buildOps,
				output.NewAnnotator(&stderr, output.FormatGitHub), &out, &stderr, in)

			if !tc.wantErr {
				require.NoErrorf(t, err, "%s", tc.why)
				require.Containsf(t, out.String(), tc.wantNote, "%s", tc.why)

				return
			}

			require.Errorf(t, err, "an archive built for %q was published as the %q release: %s", tc.recorded, in.Scheme, tc.why)
			require.ErrorIs(t, err, errs.ErrValidation)
			require.Contains(t, err.Error(), "not the selected")
		})
	}
}
