// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeprovider"
)

func writeFile(t *testing.T, dir, name string) {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("body"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestUploadAttachments_UploadsEveryMatchAndNothingElse covers the glob: both
// .tgz files are uploaded against the release tag, and the .txt beside them is
// not.
//
// The uploads are compared by name. Counting them and checking each ends in
// .tgz -- what this did before -- is satisfied by uploading the same file
// twice, or by uploading some other .tgz entirely, and says nothing about the
// file the fixture put there to be left alone.
func TestUploadAttachments_UploadsEveryMatchAndNothingElse(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "asset-a.tgz")
	writeFile(t, dir, "asset-b.tgz")
	writeFile(t, dir, "ignore.txt")

	fp := fakeprovider.New(t)
	if err := apprelease.UploadAttachments(context.Background(), fp, &bytes.Buffer{}, output.Annotator{}, apprelease.UploadAttachmentsInput{
		Tag:        "v1.0.0", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Pattern:    "*.tgz",  //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		WorkingDir: dir,
	}); err != nil {
		t.Fatal(err)
	}

	calls := fp.UploadReleaseAssetCalls()

	gotFiles := make([]string, 0, len(calls))

	for _, c := range calls { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		gotFiles = append(gotFiles, c.File)

		if c.Tag != "v1.0.0" {
			t.Errorf("file %s uploaded against tag %q, want v1.0.0", c.File, c.Tag)
		}
	}

	wantFiles := []string{filepath.Join(dir, "asset-a.tgz"), filepath.Join(dir, "asset-b.tgz")}
	if !reflect.DeepEqual(gotFiles, wantFiles) {
		t.Errorf("uploaded = %v, want %v", gotFiles, wantFiles)
	}
}

func TestUploadAttachments_NoMatchesNoOps(t *testing.T) {
	dir := t.TempDir()

	fp := fakeprovider.New(t)
	if err := apprelease.UploadAttachments(context.Background(), fp, &bytes.Buffer{}, output.Annotator{}, apprelease.UploadAttachmentsInput{
		Tag:        "v1.0.0",
		Pattern:    "*.tgz",
		WorkingDir: dir,
	}); err != nil {
		t.Fatal(err)
	}

	if got := len(fp.UploadReleaseAssetCalls()); got != 0 {
		t.Errorf("uploads = %d, want 0", got)
	}
}

func TestUploadAttachments_EmptyPatternNoOps(t *testing.T) {
	fp := fakeprovider.New(t)
	if err := apprelease.UploadAttachments(context.Background(), fp, &bytes.Buffer{}, output.Annotator{}, apprelease.UploadAttachmentsInput{
		Tag: "v1.0.0",
	}); err != nil {
		t.Fatal(err)
	}

	if got := len(fp.UploadReleaseAssetCalls()); got != 0 {
		t.Errorf("uploads = %d, want 0", got)
	}
}

func TestUploadAttachments_RejectsPathTraversal(t *testing.T) {
	fp := fakeprovider.New(t)

	err := apprelease.UploadAttachments(context.Background(), fp, &bytes.Buffer{}, output.Annotator{}, apprelease.UploadAttachmentsInput{
		Tag:     "v1.0.0",
		Pattern: "../etc/passwd",
	})
	if err == nil || !strings.Contains(err.Error(), `contains ".."`) {
		t.Fatalf("err = %v", err)
	}
}

func TestUploadAttachments_RejectsAbsolutePath(t *testing.T) {
	fp := fakeprovider.New(t)

	err := apprelease.UploadAttachments(context.Background(), fp, &bytes.Buffer{}, output.Annotator{}, apprelease.UploadAttachmentsInput{
		Tag:     "v1.0.0",
		Pattern: "/etc/passwd",
	})
	if err == nil || !strings.Contains(err.Error(), "must be relative") {
		t.Fatalf("err = %v", err)
	}
}

func TestUploadAttachments_RejectsMissingTag(t *testing.T) {
	err := apprelease.UploadAttachments(context.Background(), fakeprovider.New(t), io.Discard, output.Annotator{}, apprelease.UploadAttachmentsInput{
		Pattern: "*.tgz",
	})
	if err == nil || !strings.Contains(err.Error(), "tag is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestUploadAttachments_PartialFailureIsLoggedNotFatal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.tgz")
	writeFile(t, dir, "b.tgz")
	// All uploads fail in this fake configuration → fatal.
	fp := fakeprovider.New(t).WithUploadReleaseAssetError(errors.New("network down")) //nolint:err113 // test mock error

	err := apprelease.UploadAttachments(context.Background(), fp, &bytes.Buffer{}, output.Annotator{}, apprelease.UploadAttachmentsInput{
		Tag:        "v1.0.0",
		Pattern:    "*.tgz",
		WorkingDir: dir,
	})
	if err == nil || !strings.Contains(err.Error(), "all 2 uploads failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestUploadAttachments_SkipsDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	writeFile(t, dir, "asset.tgz")

	fp := fakeprovider.New(t)
	if err := apprelease.UploadAttachments(context.Background(), fp, &bytes.Buffer{}, output.Annotator{}, apprelease.UploadAttachmentsInput{
		Tag:        "v1.0.0",
		Pattern:    "*",
		WorkingDir: dir,
	}); err != nil {
		t.Fatal(err)
	}

	gotFiles := make([]string, 0, 1)
	for _, c := range fp.UploadReleaseAssetCalls() {
		gotFiles = append(gotFiles, c.File)
	}

	// "*" matches the directory too. It must not be handed to the provider,
	// which would try to upload it as a file.
	wantFiles := []string{filepath.Join(dir, "asset.tgz")}
	if !reflect.DeepEqual(gotFiles, wantFiles) {
		t.Errorf("uploaded = %v, want %v", gotFiles, wantFiles)
	}
}
