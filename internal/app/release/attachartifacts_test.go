// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/internal/app/release"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
)

func TestAttachArtifacts_AppendsBinariesGlobWhenFilesExist(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	binaries := filepath.Join(dir, "release-artifacts", "binaries")
	if err := os.MkdirAll(binaries, 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(binaries, "tool"), []byte("x"), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	sink := fakeoutputsink.New(t)

	var out bytes.Buffer

	got, err := apprelease.AttachArtifacts(context.Background(), sink, &out, apprelease.AttachArtifactsInput{
		UserAttach:   "dist/**",
		BinariesDir:  binaries,
		BinariesGlob: "release-artifacts/binaries/**",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "dist/**,release-artifacts/binaries/**"
	if got != want || sink.Single("attach-artifacts") != want {
		t.Errorf("got %q output %q, want %q", got, sink.Single("attach-artifacts"), want)
	}
}

func TestAttachArtifacts_PreservesUserAttachWithoutBinaries(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	got, err := apprelease.AttachArtifacts(context.Background(), sink, nil, apprelease.AttachArtifactsInput{
		UserAttach:  " dist/**\nrelease-files/*.zip ",
		BinariesDir: filepath.Join(t.TempDir(), "missing"),
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "dist/**,release-files/*.zip"
	if got != want || sink.Single("attach-artifacts") != want {
		t.Errorf("got %q output %q", got, sink.Single("attach-artifacts"))
	}
}

func TestAttachArtifacts_DoesNotDuplicateBinariesGlob(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	binaries := filepath.Join(dir, "release-artifacts", "binaries")
	if err := os.MkdirAll(binaries, 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(binaries, "tool"), []byte("x"), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	sink := fakeoutputsink.New(t)

	got, err := apprelease.AttachArtifacts(context.Background(), sink, nil, apprelease.AttachArtifactsInput{
		UserAttach:   "dist/**\nrelease-artifacts/binaries/**",
		BinariesDir:  binaries,
		BinariesGlob: "release-artifacts/binaries/**",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "dist/**,release-artifacts/binaries/**"
	if got != want || sink.Single("attach-artifacts") != want {
		t.Errorf("got %q output %q, want %q", got, sink.Single("attach-artifacts"), want)
	}
}
