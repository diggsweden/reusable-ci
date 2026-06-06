// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/internal/app/container"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestValidateContainerfile_ExactPathExists(t *testing.T) {
	fsys := testfs.NewReal(t)
	path := fsys.WriteFile("Containerfile", []byte("FROM alpine"))
	sink := fakeoutputsink.New(t)

	var out bytes.Buffer
	if err := appcontainer.ValidateContainerfile(context.Background(), sink, &out, appcontainer.ValidateContainerfileInput{Path: path}); err != nil {
		t.Fatalf("ValidateContainerfile: %v", err)
	}

	if sink.Single("containerfile") != path {
		t.Errorf("containerfile = %q", sink.Single("containerfile"))
	}
}

func TestValidateContainerfile_SingleGlobMatch(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("Containerfile.builder", []byte("FROM alpine"))

	sink := fakeoutputsink.New(t)

	var out bytes.Buffer
	if err := appcontainer.ValidateContainerfile(context.Background(), sink, &out, appcontainer.ValidateContainerfileInput{
		Path: fsys.Path("Containerfile"), // does not exist; glob picks up .builder
	}); err != nil {
		t.Fatalf("ValidateContainerfile: %v", err)
	}

	if !strings.HasSuffix(sink.Single("containerfile"), "Containerfile.builder") {
		t.Errorf("containerfile = %q", sink.Single("containerfile"))
	}
}

func TestValidateContainerfile_MultipleMatchesErrors(t *testing.T) {
	fsys := testfs.NewReal(t)
	for _, name := range []string{"Containerfile.builder", "Dockerfile.runner"} {
		fsys.WriteFile(name, []byte("FROM alpine"))
	}

	err := appcontainer.ValidateContainerfile(context.Background(), fakeoutputsink.New(t), io.Discard, appcontainer.ValidateContainerfileInput{
		Path: fsys.Path("Containerfile"),
	})
	if err == nil || !strings.Contains(err.Error(), "multiple containerfiles") {
		t.Errorf("expected multi-match error, got: %v", err)
	}
}

func TestValidateContainerfile_NoMatchErrors(t *testing.T) {
	fsys := testfs.NewReal(t)

	err := appcontainer.ValidateContainerfile(context.Background(), fakeoutputsink.New(t), io.Discard, appcontainer.ValidateContainerfileInput{
		Path: fsys.Path("Containerfile"),
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got: %v", err)
	}
}

func TestValidateContainerfile_EmptyPathErrors(t *testing.T) {
	if err := appcontainer.ValidateContainerfile(context.Background(), fakeoutputsink.New(t), io.Discard, appcontainer.ValidateContainerfileInput{}); err == nil {
		t.Fatal("expected error")
	}
}
