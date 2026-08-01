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

func TestXcodeReleaseBuild_UnsignedSequence(t *testing.T) {
	t.Chdir(t.TempDir()) // version-info walks cwd for *.xcodeproj (none → "unknown")

	buildOps := &fakeXcodeBuild{}
	sink := fakeoutputsink.New(t)

	err := appbuild.XcodeReleaseBuild(context.Background(), sink, &fakeSecurity{}, buildOps, output.Annotator{}, io.Discard, io.Discard, appbuild.XcodeReleaseBuildInput{
		RepositoryName:    "app",
		Scheme:            "MyApp",
		Configuration:     "Release",
		Destination:       "generic/platform=iOS",
		EnableCodeSigning: false, // no signing → no export, no keychain ops
	})
	if err != nil {
		t.Fatal(err)
	}

	// ipa-name + version emitted as job outputs.
	if sink.Single("ipa-name") == "" || sink.Single("version") == "" {
		t.Errorf("ipa-name/version outputs not emitted")
	}

	// Archive ran with the scheme; no IPA export (unsigned).
	joined := xcodeCalls(buildOps.calls)
	if !strings.Contains(joined, "MyApp") {
		t.Errorf("archive not run with scheme: %s", joined)
	}

	if strings.Contains(joined, "-exportArchive") {
		t.Errorf("export ran despite EnableCodeSigning=false: %s", joined)
	}
}

func TestXcodeReleaseBuild_ArchiveErrorPropagates(t *testing.T) {
	t.Chdir(t.TempDir())

	err := appbuild.XcodeReleaseBuild(context.Background(), fakeoutputsink.New(t), &fakeSecurity{}, &fakeXcodeBuild{}, output.Annotator{}, io.Discard, io.Discard, appbuild.XcodeReleaseBuildInput{
		RepositoryName:    "app",
		EnableCodeSigning: false,
		// no scheme/configuration/destination → archive fails
	})
	if err == nil || !strings.Contains(err.Error(), "archive") {
		t.Fatalf("err = %v, want archive error", err)
	}
}

func xcodeCalls(calls [][]string) string {
	parts := make([]string, 0, len(calls))
	for _, c := range calls {
		parts = append(parts, strings.Join(c, " "))
	}

	return strings.Join(parts, " ")
}
