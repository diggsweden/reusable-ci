//go:build unix

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// TestMergeManifest_RefusesANamedPipeMarker covers a marker that is neither a
// file nor a link: a FIFO. Opening one blocks, so it must be refused from its
// directory entry, with no registry call.
func TestMergeManifest_RefusesANamedPipeMarker(t *testing.T) {
	t.Parallel()

	fsys := testfs.NewReal(t)
	dir := fsys.MkdirAll("digests")

	if err := syscall.Mkfifo(filepath.Join(dir, strings.Repeat("c", 64)), 0o600); err != nil {
		t.Skipf("cannot create a FIFO here: %v", err)
	}

	reg := &fakeManifestRegistry{}

	err := appcontainer.MergeManifest(context.Background(), reg, io.Discard, appcontainer.MergeManifestInput{
		ImageName: "ghcr.io/org/app", Tags: "ghcr.io/org/app:v1", DigestsDir: dir,
	})
	if !errors.Is(err, errs.ErrValidation) || len(reg.events) != 0 {
		t.Errorf("err = %v, registry calls = %q; want ErrValidation and none", err, reg.events)
	}
}
