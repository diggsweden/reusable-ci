// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
	"github.com/stretchr/testify/require"
)

// The Xcode release build buffers every output and publishes them in one flush
// at the end, so that a failed build never leaves half its outputs visible to
// later steps. The buffering is the mechanism; the property is that publication
// is all-or-nothing.
//
// A failing sink is what tells the two apart. If the flush stops at the first
// error, some keys are already set and some never will be, and a later step
// reading them sees a build that half happened. That is worse than no outputs:
// a missing key fails loudly, a partial set is acted on.
//
// This does not claim the sink itself is transactional — a runner's output file
// is append-only and this cannot un-append. What it pins is that the failure
// reaches the caller rather than being swallowed, and that the build reports
// failure rather than success with an incomplete output set.

var errSinkRefused = errors.New("output sink refused the write") //nolint:err113 // injected identity is the contract.

// failAtSink fails the nth Set/SetBool/SetMultiline call, counting from one,
// so a test can place the failure anywhere in the flush.
type failAtSink struct {
	failAt int
	calls  int
	keys   []string
}

func (s *failAtSink) Set(_ context.Context, key, _ string) error { return s.record(key) }
func (s *failAtSink) SetBool(_ context.Context, key string, _ bool) error {
	return s.record(key)
}

func (s *failAtSink) SetMultiline(_ context.Context, key string, _ []string) error {
	return s.record(key)
}
func (s *failAtSink) Close(context.Context) error { return nil }

func (s *failAtSink) record(key string) error {
	s.calls++
	if s.calls == s.failAt {
		return errSinkRefused
	}

	s.keys = append(s.keys, key)

	return nil
}

var _ ci.OutputSink = (*failAtSink)(nil)

// No t.Parallel: xcodeReleaseFixture uses t.Setenv and t.Chdir.
func TestXcodeReleaseBuild_AFailingOutputSinkFailsTheBuild(t *testing.T) {
	// One successful run first, to learn how many outputs a build publishes.
	// Hard-coding the count would go stale the day an output is added, and the
	// matrix below would quietly stop covering the last position.
	positions := xcodeOutputCount(t)
	require.GreaterOrEqual(t, positions, 2,
		"a build published %d outputs; a failure matrix over that proves little", positions)

	for position := 1; position <= positions; position++ {
		t.Run("sink fails at output "+itoaBuild(position), func(t *testing.T) {
			fsys, in := unsignedXcodeRelease(t)

			buildOps := &fakeXcodeBuild{run: func(args []string) {
				if len(args) > 0 && args[0] == "archive" {
					fsys.WriteFile("checkout/build/app.xcarchive/Info.plist", []byte(xcarchiveInfoPlist(in.Scheme)))
				}
			}}

			sink := &failAtSink{failAt: position}

			var out, stderr strings.Builder

			err := appbuild.XcodeReleaseBuild(t.Context(), sink, unsignedXcodeSecurity{t}, buildOps,
				output.NewAnnotator(&stderr, output.FormatGitHub), &out, &stderr, in)

			require.Errorf(t, err, "a build whose output %d could not be published reported success", position)
			require.ErrorIs(t, err, errSinkRefused, "the sink's cause was replaced or swallowed")
			require.Lenf(t, sink.keys, position-1,
				"the flush continued past a failing write; a later step would read a partial output set")
		})
	}
}

// xcodeOutputCount runs one successful build and reports how many outputs it
// published.
func xcodeOutputCount(t *testing.T) int {
	t.Helper()

	fsys, in := unsignedXcodeRelease(t)

	buildOps := &fakeXcodeBuild{run: func(args []string) {
		if len(args) > 0 && args[0] == "archive" {
			fsys.WriteFile("checkout/build/app.xcarchive/Info.plist", []byte(xcarchiveInfoPlist(in.Scheme)))
		}
	}}

	sink := &failAtSink{failAt: -1}

	var out, stderr strings.Builder

	require.NoError(t, appbuild.XcodeReleaseBuild(t.Context(), sink, unsignedXcodeSecurity{t}, buildOps,
		output.NewAnnotator(&stderr, output.FormatGitHub), &out, &stderr, in))

	return sink.calls
}

// unsignedXcodeRelease is the shared release fixture with signing off, so these
// tests are about output publication rather than the keychain lifecycle.
func unsignedXcodeRelease(t *testing.T) (*testfs.Real, appbuild.XcodeReleaseBuildInput) {
	t.Helper()

	fsys, input := xcodeReleaseFixture(t)
	input.EnableCodeSigning = false
	input.CertBase64, input.PPBase64 = "", ""
	input.CertPassphrase, input.KeychainPassword = "", ""

	return fsys, input
}

func itoaBuild(n int) string {
	if n == 0 {
		return "0"
	}

	digits := ""
	for ; n > 0; n /= 10 {
		digits = string(rune('0'+n%10)) + digits
	}

	return digits
}
