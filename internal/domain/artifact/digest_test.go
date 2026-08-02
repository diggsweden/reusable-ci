// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package artifact_test

import (
	"io/fs"
	"testing"
	"testing/fstest"

	domainartifact "github.com/diggsweden/reusable-ci/v3/internal/domain/artifact"
)

func digest(t *testing.T, fsys fs.FS) string {
	t.Helper()

	sum, err := domainartifact.Digest(fsys, ".")
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}

	if len(sum) != 64 {
		t.Fatalf("digest %q is not 64 hex chars", sum)
	}

	return sum
}

func TestDigest_StableAndReproducible(t *testing.T) {
	fsys := fstest.MapFS{
		"dist/app":           {Data: []byte("binary"), Mode: 0o755},
		"dist/sub/checksums": {Data: []byte("sums\n"), Mode: 0o644},
		"dist/empty":         {Data: []byte{}, Mode: 0o644},
	}

	if got, want := digest(t, fsys), digest(t, fsys); got != want {
		t.Errorf("digest not stable: %s != %s", got, want)
	}
}

// TestDigest_CanonicalGolden pins the exact canonical digest for a fixed tree.
// It locks the CLI's canonical value the way forgejo-ci's golden-baseline-*
// self-tests lock the bash side: the build->sign tamper-evidence contract must
// not change silently, and at the (atomic) migration flip the CLI side is
// diffed against this pin. A change here means the canonical scheme changed —
// update intentionally and re-confirm downstream verifiers.
func TestDigest_CanonicalGolden(t *testing.T) {
	fsys := fstest.MapFS{
		"dist/a.txt":     {Data: []byte("alpha"), Mode: 0o644},
		"dist/sub/b.txt": {Data: []byte("bravo"), Mode: 0o644},
		"dist/empty":     {Data: []byte{}, Mode: 0o644},
	}

	sum, err := domainartifact.Digest(fsys, "dist")
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}

	const want = "c8dc21c08e4283f6c5fd341946e8274f66722aed0a3fd69a8ba4360de26a9430"
	if sum != want {
		t.Errorf("canonical digest drifted:\n got=%s\nwant=%s", sum, want)
	}
}

func TestDigest_ContentChangeChangesDigest(t *testing.T) {
	base := fstest.MapFS{"dist/a": {Data: []byte("one"), Mode: 0o644}}
	changed := fstest.MapFS{"dist/a": {Data: []byte("two"), Mode: 0o644}}

	if digest(t, base) == digest(t, changed) {
		t.Error("digest did not change when content changed")
	}
}

func TestDigest_ExecBitIsSignificant(t *testing.T) {
	plain := fstest.MapFS{"dist/a": {Data: []byte("same"), Mode: 0o644}}
	exec := fstest.MapFS{"dist/a": {Data: []byte("same"), Mode: 0o755}}

	if digest(t, plain) == digest(t, exec) {
		t.Error("digest ignored the executable bit")
	}
}

func TestDigest_PathIsSignificant(t *testing.T) {
	here := fstest.MapFS{"dist/a": {Data: []byte("x"), Mode: 0o644}}
	there := fstest.MapFS{"dist/b": {Data: []byte("x"), Mode: 0o644}}

	if digest(t, here) == digest(t, there) {
		t.Error("digest ignored the file path")
	}
}
