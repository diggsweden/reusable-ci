// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
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

// TestValidateContainerfile_MultipleMatchesErrors requires the refusal to name
// every candidate, in a stable order.
//
// It used to assert only the substring "multiple containerfiles", which an
// error listing none of them satisfies. That message is the operator's whole
// remedy: the fix is to pass an exact path, and they cannot choose one without
// being told which files collided. Order is asserted because the message is
// read in a CI log and compared between runs; candidates that move between
// runs read as a changing failure.
//
// The candidates are created in reverse of the expected order. os.ReadDir
// already returns entries sorted by filename, so what the explicit sort in the
// resolver defends against is a change of directory-reading API, not this
// fixture — what this pins is the message, and that both names reach it.
func TestValidateContainerfile_MultipleMatchesErrors(t *testing.T) {
	fsys := testfs.NewReal(t)
	for _, name := range []string{"Dockerfile.runner", "Containerfile.builder"} {
		fsys.WriteFile(name, []byte("FROM alpine"))
	}

	err := appcontainer.ValidateContainerfile(context.Background(), fakeoutputsink.New(t), io.Discard, appcontainer.ValidateContainerfileInput{
		Path: fsys.Path("Containerfile"),
	})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "multiple containerfiles") {
		t.Fatalf("err = %v, want ErrValidation listing the ambiguous matches", err)
	}

	msg := err.Error()

	builder, runner := fsys.Path("Containerfile.builder"), fsys.Path("Dockerfile.runner")

	for _, want := range []string{builder, runner} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not name the candidate %q:\n%s", want, msg)
		}
	}

	if at, other := strings.Index(msg, builder), strings.Index(msg, runner); at >= 0 && other >= 0 && at > other {
		t.Errorf("candidates are not listed in sorted order:\n%s", msg)
	}
}

func TestValidateContainerfile_NoMatchErrors(t *testing.T) {
	fsys := testfs.NewReal(t)

	err := appcontainer.ValidateContainerfile(context.Background(), fakeoutputsink.New(t), io.Discard, appcontainer.ValidateContainerfileInput{
		Path: fsys.Path("Containerfile"),
	})
	if !errors.Is(err, errs.ErrMissingInput) || !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v, want ErrMissingInput", err)
	}
}

func TestValidateContainerfile_EmptyPathErrors(t *testing.T) {
	sink := fakeoutputsink.New(t)

	err := appcontainer.ValidateContainerfile(context.Background(), sink, io.Discard, appcontainer.ValidateContainerfileInput{})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	// No containerfile output: a later step reading an empty path would
	// build from whatever the daemon's default resolution finds.
	if got := sink.Keys(); len(got) != 0 {
		t.Errorf("emitted %q with no path given", got)
	}
}
