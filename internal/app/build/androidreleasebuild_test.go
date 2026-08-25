// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"context"
	"errors"
	"io"
	"reflect"
	"regexp"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

func TestAndroidReleaseBuild_RunsSequenceAndEmitsNames(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		buildTypes string
		wantTasks  []string
	}{
		{
			name:       "release only",
			buildTypes: "release",
			wantTasks:  []string{"assembleRelease"},
		},
		{
			// Debug first: the tasks are ordered by the resolver, not by
			// the order they were named.
			name:       "debug and release in one invocation",
			buildTypes: "release debug",
			wantTasks:  []string{"assembleDebug", "assembleRelease"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ops := &recordingGradle{}
			sink := fakeoutputsink.New(t)

			err := appbuild.AndroidReleaseBuild(context.Background(), sink, &recordingSummarySink{}, ops, output.Annotator{}, io.Discard, io.Discard, appbuild.AndroidReleaseBuildInput{
				ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newGradleDir(t), EnableBuildSBOM: false},
				IncludeDateStamp:    false,
				RepoName:            "app",
				BuildTypes:          tc.buildTypes,
				BuildModule:         "app",
			})
			if err != nil {
				t.Fatal(err)
			}

			// The names the workflow consumes, by value. The previous
			// assertion only checked they were non-empty, which the
			// fallback "unknown" satisfies too -- so a total failure to
			// resolve a version passed as success.
			wantOutputs := map[string]string{
				"release-name": "app - APK release",
				"debug-name":   "app - APK debug",
				"aab-name":     "app - AAB release",
				"aar-name":     "app - AAR release",
				"sbom-name":    "app - build SBOM",
				"version":      "unknown", // no build.gradle in the fixture
				"version-code": "unknown",
			}
			if got := sink.AllScalar(); !reflect.DeepEqual(got, wantOutputs) {
				t.Errorf("outputs =\n%v\nwant\n%v", got, wantOutputs)
			}

			// One gradle invocation carrying every task.
			assertGradleCalls(t, ops.calls, tc.wantTasks)
		})
	}
}

// TestAndroidReleaseBuild_DateStampPrefixesEveryName covers the naming
// knob that had no test. The stamp is today's date, so the shape is
// asserted rather than a literal.
func TestAndroidReleaseBuild_DateStampPrefixesEveryName(t *testing.T) {
	t.Parallel()

	sink := fakeoutputsink.New(t)

	err := appbuild.AndroidReleaseBuild(context.Background(), sink, &recordingSummarySink{}, &recordingGradle{}, output.Annotator{}, io.Discard, io.Discard, appbuild.AndroidReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newGradleDir(t), EnableBuildSBOM: false},
		IncludeDateStamp:    true,
		RepoName:            "app",
		BuildTypes:          "release",
		BuildModule:         "app",
	})
	if err != nil {
		t.Fatal(err)
	}

	stamped := regexp.MustCompile(`^\d{4}-\d{2}-\d{2} - app - `)
	for _, key := range []string{"release-name", "debug-name", "aab-name", "sbom-name"} {
		if got := sink.Single(key); !stamped.MatchString(got) {
			t.Errorf("%s = %q, want a date-stamped name", key, got)
		}
	}
}

func TestAndroidReleaseBuild_SigningWithoutKeystoreFails(t *testing.T) {
	t.Parallel()

	ops := &recordingGradle{}
	sink := fakeoutputsink.New(t)
	summary := &recordingSummarySink{}

	err := appbuild.AndroidReleaseBuild(context.Background(), sink, summary, ops, output.Annotator{}, io.Discard, io.Discard, appbuild.AndroidReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newGradleDir(t)},
		RepoName:            "app",
		EnableSigning:       true, // no keystore-base64
	})

	// ErrPermissionDenied, not a generic failure: a missing signing secret
	// exits EX_NOPERM so the workflow can tell it from a build error.
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}

	// Refused before building: signing that cannot happen must not produce
	// an unsigned APK that looks like a release.
	if len(ops.calls) != 0 {
		t.Errorf("ran gradle without a keystore: %v", ops.calls)
	}

	if summary.buf.Len() != 0 {
		t.Errorf("wrote a summary for a build that never started: %q", summary.buf.String())
	}
}
