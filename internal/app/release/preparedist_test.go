// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

func TestPrepareDist_PrunesDirectoriesAndEmitsDigest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writePrepareDistFile(t, filepath.Join(dir, "release.txt"), "release\n")
	writePrepareDistFile(t, filepath.Join(dir, "raw", "binary"), "pruned\n")

	sink := fakeoutputsink.New(t)

	var stderr bytes.Buffer

	got, err := apprelease.PrepareDist(context.Background(), sink, &stderr, apprelease.PrepareDistInput{Path: dir, PruneDirs: true})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, "raw")); !os.IsNotExist(err) {
		t.Fatalf("top-level directory was not pruned, stat err=%v", err)
	}

	if got.Digest == "" || sink.Single("digest") != got.Digest {
		t.Fatalf("digest output mismatch: result=%q sink=%q", got.Digest, sink.Single("digest"))
	}

	if stderr.String() == "" {
		t.Fatal("expected digest log on stderr")
	}
}

func TestPrepareDist_RejectsUnsafePruneRoot(t *testing.T) {
	t.Parallel()

	_, err := apprelease.PrepareDist(context.Background(), nil, &bytes.Buffer{}, apprelease.PrepareDistInput{Path: ".", PruneDirs: true})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

func writePrepareDistFile(t *testing.T, path, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil { //nolint:gosec // test-owned path.
		t.Fatal(err)
	}
}
