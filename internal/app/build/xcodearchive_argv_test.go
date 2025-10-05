// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
)

// Every value the archive step accepts becomes an xcodebuild argument. Two
// questions follow from that and neither was asked directly.
//
// The first is whether a refusal happens BEFORE the tool runs. A refusal that
// arrives after xcodebuild started has already spent the build; worse, on the
// signing path it has already unlocked a keychain.
//
// The second is whether a value survives as ONE argument. A scheme called
// "My App" is ordinary on macOS, and the difference between passing it as one
// element and as two is the difference between building it and building
// something else — or nothing. Go's exec does not split on spaces, so this
// holds by construction, and that is exactly why it is worth a test: the day
// someone builds the argv by joining strings, nothing else would notice.

// No t.Parallel: xcodeReleaseFixture uses t.Setenv and t.Chdir.
func TestXcodeArchive_RefusalsRunNoTool(t *testing.T) {
	valid := func() appbuild.XcodeArchiveInput {
		return appbuild.XcodeArchiveInput{
			Project: "Sources/Zebra.xcodeproj", Scheme: "Zebra",
			Configuration: "Release", Destination: "generic/platform=iOS",
		}
	}

	for _, tc := range []struct {
		name    string
		mutate  func(*appbuild.XcodeArchiveInput)
		wantErr error
		why     string
	}{
		{
			name: "neither workspace nor project", why: "there is no identity to build",
			mutate: func(in *appbuild.XcodeArchiveInput) { in.Project = "" },
		},
		{
			name: "both workspace and project", why: "xcodebuild would silently prefer one; the caller meant something specific",
			mutate: func(in *appbuild.XcodeArchiveInput) { in.Workspace = "App.xcworkspace" },
		},
		{
			name: "a project with the wrong extension", why: "a .xcworkspace passed as --project is a different tool mode",
			mutate: func(in *appbuild.XcodeArchiveInput) { in.Project = "App.xcworkspace" },
		},
		{
			name: "an absent project", wantErr: errs.ErrMissingInput,
			why:    "the failure should name the missing directory, not come back as a tool error",
			mutate: func(in *appbuild.XcodeArchiveInput) { in.Project = "Nowhere.xcodeproj" },
		},
		{
			name: "no scheme", why: "required and not defaultable",
			mutate: func(in *appbuild.XcodeArchiveInput) { in.Scheme = "" },
		},
		{
			name: "no configuration", why: "required and not defaultable",
			mutate: func(in *appbuild.XcodeArchiveInput) { in.Configuration = "" },
		},
		{
			name: "no destination", why: "required and not defaultable",
			mutate: func(in *appbuild.XcodeArchiveInput) { in.Destination = "" },
		},
		{
			name: "a NUL in the scheme", why: "a NUL truncates the argument at the syscall boundary",
			mutate: func(in *appbuild.XcodeArchiveInput) { in.Scheme = "Zebra\x00extra" },
		},
		{
			name: "a NUL in the build number", why: "same, on a value that reaches argv as a setting assignment",
			mutate: func(in *appbuild.XcodeArchiveInput) { in.BuildNumber = "999\x00" },
		},
		{
			name: "an unreadable xcconfig", wantErr: errs.ErrMissingInput,
			why:    "the archive would run with settings nobody could read",
			mutate: func(in *appbuild.XcodeArchiveInput) { in.XcconfigPath = "absent.xcconfig" },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys, _ := xcodeReleaseFixture(t)
			_ = fsys

			in := valid()
			tc.mutate(&in)

			ops := &fakeXcodeBuild{run: func(args []string) {
				t.Errorf("xcodebuild ran despite a refusable input: %q", args)
			}}

			var out, stderr strings.Builder

			want := tc.wantErr
			if want == nil {
				want = errs.ErrUsage
			}

			err := appbuild.XcodeArchive(t.Context(), ops, &out, &stderr, in)
			require.Errorf(t, err, "the input was accepted: %s", tc.why)
			require.ErrorIsf(t, err, want, "%s", tc.why)
		})
	}
}

// A value containing spaces or shell metacharacters must arrive as one argv
// element, unquoted and unsplit. Nothing here goes through a shell, so the
// metacharacters are ordinary bytes — and a test that says so is what would
// catch a future argv built by string concatenation.
// No t.Parallel: xcodeReleaseFixture uses t.Setenv and t.Chdir.
func TestXcodeArchive_AwkwardValuesSurviveAsSingleArguments(t *testing.T) {
	for _, tc := range []struct {
		name   string
		scheme string
		config string
		why    string
	}{
		{name: "spaces", scheme: "My App", config: "Release Candidate", why: "ordinary on macOS"},
		{name: "shell metacharacters", scheme: "App;rm -rf /", config: "Rel$(whoami)", why: "no shell is involved, so these are just bytes"},
		{name: "quotes", scheme: `App "Prod"`, config: "Rel'ease", why: "quoting is a shell concept, not an exec one"},
		{name: "a leading dash", scheme: "-App", config: "-Release", why: "xcodebuild reads it as a value because it follows its flag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys, _ := xcodeReleaseFixture(t)
			_ = fsys

			var seen []string

			ops := &fakeXcodeBuild{run: func(args []string) { seen = append([]string(nil), args...) }}

			var out, stderr strings.Builder

			require.NoError(t, appbuild.XcodeArchive(t.Context(), ops, &out, &stderr, appbuild.XcodeArchiveInput{
				Project: "Sources/Zebra.xcodeproj", Scheme: tc.scheme,
				Configuration: tc.config, Destination: "generic/platform=iOS",
			}))

			require.Equalf(t, tc.scheme, argAfter(t, seen, "-scheme"),
				"the scheme did not survive as one argument: %s", tc.why)
			require.Equalf(t, tc.config, argAfter(t, seen, "-configuration"),
				"the configuration did not survive as one argument: %s", tc.why)
		})
	}
}

func argAfter(t *testing.T, args []string, flag string) string {
	t.Helper()

	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}

	t.Fatalf("%s not found in %q", flag, args)

	return ""
}
