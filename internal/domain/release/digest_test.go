// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

// These exercise the hand-off rule over an in-memory tree: no temp dir, no
// runner, no os. The byte-compatibility contract against the real shell
// pipeline is pinned in internal/app/release (which has a filesystem to
// compare against) and in the black-box suite.

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))

	return hex.EncodeToString(sum[:])
}

// TestDistDigest_ManifestShape pins the exact bytes hashed: the sha256sum
// manifest, sorted by manifest path, with "." producing the "./name" paths
// `cd <dir> && find .` emits.
func TestDistDigest_ManifestShape(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"b.txt":        {Data: []byte("bee")},
		"a.txt":        {Data: []byte("ay")},
		"nested/c.txt": {Data: []byte("cee")},
	}

	got, err := release.DistDigest(fsys, ".")
	if err != nil {
		t.Fatalf("DistDigest: %v", err)
	}

	// Sorted by manifest path: ./a.txt, ./b.txt, ./nested/c.txt.
	manifest := sha256Hex("ay") + "  ./a.txt\n" +
		sha256Hex("bee") + "  ./b.txt\n" +
		sha256Hex("cee") + "  ./nested/c.txt\n"

	if want := sha256Hex(manifest); got != want {
		t.Errorf("digest = %s, want %s (manifest bytes disagree)", got, want)
	}
}

// TestDistDigest_ManifestRootCommitsToPrefix: the prefix is hashed, so a
// producer and a verifier that disagree about it disagree about the digest.
// This is why both sides pin --manifest-root.
func TestDistDigest_ManifestRootCommitsToPrefix(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{"app.tar.gz": {Data: []byte("payload")}}

	dot, err := release.DistDigest(fsys, ".")
	if err != nil {
		t.Fatalf("DistDigest(.): %v", err)
	}

	named, err := release.DistDigest(fsys, "dist")
	if err != nil {
		t.Fatalf("DistDigest(dist): %v", err)
	}

	if dot == named {
		t.Error("manifest root is not committed to; producer and verifier could disagree undetected")
	}
}

// TestDistDigest_DetectsContentChange: the whole point of the hand-off.
func TestDistDigest_DetectsContentChange(t *testing.T) {
	t.Parallel()

	before, err := release.DistDigest(fstest.MapFS{"a.txt": {Data: []byte("payload")}}, ".")
	if err != nil {
		t.Fatalf("DistDigest: %v", err)
	}

	after, err := release.DistDigest(fstest.MapFS{"a.txt": {Data: []byte("payloadX")}}, ".")
	if err != nil {
		t.Fatalf("DistDigest: %v", err)
	}

	if before == after {
		t.Error("a content change must flip the digest")
	}
}

// TestDistDigest_RejectsEmptyTree: an empty hand-off is a producer bug, and
// digesting it to the empty-tree constant would let that bug verify.
func TestDistDigest_RejectsEmptyTree(t *testing.T) {
	t.Parallel()

	_, err := release.DistDigest(fstest.MapFS{}, ".")
	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("empty tree: err = %v, want ErrValidation", err)
	}
}

func TestWalkSafe(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		fsys    fs.FS
		wantErr bool
	}{
		"plain tree": {
			fsys: fstest.MapFS{"a.txt": {Data: []byte("ok")}, "nested/b.txt": {Data: []byte("ok")}},
		},
		"symlink": {
			fsys:    fstest.MapFS{"a.txt": {Data: []byte("ok")}, "evil": {Mode: fs.ModeSymlink}},
			wantErr: true,
		},
		"non-regular entry": {
			fsys:    fstest.MapFS{"a.txt": {Data: []byte("ok")}, "dev": {Mode: fs.ModeDevice}},
			wantErr: true,
		},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := release.WalkSafe(testCase.fsys)
			if testCase.wantErr && !errors.Is(err, errs.ErrValidation) {
				t.Errorf("err = %v, want ErrValidation", err)
			}

			if !testCase.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
