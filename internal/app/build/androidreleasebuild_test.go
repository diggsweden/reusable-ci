// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"context"
	"io"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

func TestAndroidReleaseBuild_RunsSequenceAndEmitsNames(t *testing.T) {
	t.Parallel()
	dir := newGradleDir(t) // provides gradlew + gradle.properties
	ops := &recordingGradle{}
	sink := fakeoutputsink.New(t)

	err := appbuild.AndroidReleaseBuild(context.Background(), sink, &recordingSummarySink{}, ops, output.Annotator{}, io.Discard, io.Discard, appbuild.AndroidReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir, EnableBuildSBOM: false},
		IncludeDateStamp:    false,
		RepoName:            "app",
		BuildTypes:          "release",
		BuildModule:         "app",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Artifact names emitted as job outputs.
	if sink.Single("release-name") == "" || sink.Single("sbom-name") == "" {
		t.Errorf("artifact-name outputs not emitted: %+v", sink)
	}

	// Version emitted.
	if sink.Single("version") == "" {
		t.Errorf("version output not emitted")
	}

	// Gradle build ran the resolved release task.
	joined := strings.Join(ops.calls[0], " ")
	if !strings.Contains(strings.Join(flattenGradle(ops.calls), "\n"), "assembleRelease") {
		t.Errorf("expected assembleRelease in gradle calls, got: %s", joined)
	}
}

func TestAndroidReleaseBuild_SigningWithoutKeystoreFails(t *testing.T) {
	t.Parallel()

	err := appbuild.AndroidReleaseBuild(context.Background(), fakeoutputsink.New(t), &recordingSummarySink{}, &recordingGradle{}, output.Annotator{}, io.Discard, io.Discard, appbuild.AndroidReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newGradleDir(t)},
		RepoName:            "app",
		EnableSigning:       true, // no keystore-base64
	})
	if err == nil || !strings.Contains(err.Error(), "ANDROID_KEYSTORE") {
		t.Fatalf("err = %v, want keystore-required error", err)
	}
}

func flattenGradle(calls [][]string) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, strings.Join(c, " "))
	}

	return out
}
