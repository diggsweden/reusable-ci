// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
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

	// By value. Checking these were merely non-empty passed on "unknown",
	// the value they take when no .xcodeproj is found -- so a complete
	// failure to resolve a version read as success.
	wantOutputs := map[string]string{
		"ipa-name": "app",
		"version":  "unknown",
		"build":    "unknown",
	}
	if got := sink.AllScalar(); !reflect.DeepEqual(got, wantOutputs) {
		t.Errorf("outputs = %v, want %v", got, wantOutputs)
	}

	// Exactly one xcodebuild invocation, pinned whole. Unsigned means no
	// -exportArchive, which the old assertion checked by searching a
	// string built from every call joined together -- so it could not tell
	// which invocation anything belonged to.
	wantArchive := []string{
		"archive",
		"-scheme", "MyApp",
		"-configuration", "Release",
		"-archivePath", filepath.Join("build", "app.xcarchive"),
		"-destination", "generic/platform=iOS",
		"-skipPackagePluginValidation",
	}

	if len(buildOps.calls) != 1 {
		t.Fatalf("xcodebuild calls = %v, want exactly the archive", buildOps.calls)
	}

	if !reflect.DeepEqual(buildOps.calls[0], wantArchive) {
		t.Errorf("archive = %q, want %q", buildOps.calls[0], wantArchive)
	}
}

func TestXcodeReleaseBuild_ArchiveErrorPropagates(t *testing.T) {
	t.Chdir(t.TempDir())

	ops := &fakeXcodeBuild{}
	sink := fakeoutputsink.New(t)

	err := appbuild.XcodeReleaseBuild(context.Background(), sink, &fakeSecurity{}, ops, output.Annotator{}, io.Discard, io.Discard, appbuild.XcodeReleaseBuildInput{
		RepositoryName:    "app",
		EnableCodeSigning: false,
		// no scheme/configuration/destination → archive fails
	})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	// Refused before xcodebuild is reached.
	if len(ops.calls) != 0 {
		t.Errorf("ran xcodebuild without a scheme: %v", ops.calls)
	}

	// Recorded, not endorsed: metadata is emitted before the archive is
	// attempted, so a failed run still publishes an ipa-name for a file
	// that was never produced. See docs/open-questions.md.
	if got := sink.Keys(); !reflect.DeepEqual(got, []string{"build", "ipa-name", "version"}) {
		t.Errorf("outputs on a failed archive = %v", got)
	}
}
