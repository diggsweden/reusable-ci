// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package artifact_test

import (
	"errors"
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
	t.Parallel()

	fsys := fstest.MapFS{
		"dist/app":           {Data: []byte("binary"), Mode: 0o755},
		"dist/sub/checksums": {Data: []byte("sums\n"), Mode: 0o644},
		"dist/empty":         {Data: []byte{}, Mode: 0o644},
	}

	if got, want := digest(t, fsys), digest(t, fsys); got != want {
		t.Errorf("digest not stable: %s != %s", got, want)
	}
}

// TestDigest_CanonicalGolden pins the exact digest artifact.Digest produces
// for a fixed tree, so a change to its own canonical scheme -- the manifest
// line format, the ordering, what it commits to -- cannot land silently. The
// expected value is written out rather than derived from product constants,
// which would let the test follow the scheme it is meant to pin.
//
// This comment used to call the value the build->sign tamper-evidence
// contract. It is not: that hand-off uses release.DistDigest, a different,
// sha256sum-compatible scheme, and the product documentation above Digest says
// so. A caller binding a hand-off wants release.DistDigest, whose manifest
// bytes are pinned in internal/domain/release and whose compatibility with the
// shell pipeline is pinned in internal/app/release; this value concerns
// `reusable-ci artifact digest` and the adopters who use it directly. A change
// here means that scheme changed and those adopters' recorded values stop
// matching.
func TestDigest_CanonicalGolden(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	base := fstest.MapFS{"dist/a": {Data: []byte("one"), Mode: 0o644}}
	changed := fstest.MapFS{"dist/a": {Data: []byte("two"), Mode: 0o644}}

	if digest(t, base) == digest(t, changed) {
		t.Error("digest did not change when content changed")
	}
}

func TestDigest_ExecBitIsSignificant(t *testing.T) {
	t.Parallel()

	plain := fstest.MapFS{"dist/a": {Data: []byte("same"), Mode: 0o644}}
	exec := fstest.MapFS{"dist/a": {Data: []byte("same"), Mode: 0o755}}

	if digest(t, plain) == digest(t, exec) {
		t.Error("digest ignored the executable bit")
	}
}

func TestDigest_PathIsSignificant(t *testing.T) {
	t.Parallel()

	here := fstest.MapFS{"dist/a": {Data: []byte("x"), Mode: 0o644}}
	there := fstest.MapFS{"dist/b": {Data: []byte("x"), Mode: 0o644}}

	if digest(t, here) == digest(t, there) {
		t.Error("digest ignored the file path")
	}
}

// TestDigest_AnyExecuteBitIsTheSameCanonicalIdentity pins the documented rule:
// the mode field is "0755" when ANY execute bit is set, "0644" otherwise.
//
// The existing exec-bit test compares 0o644 against 0o755, and 0o755 carries
// all three execute bits — so narrowing the check from 0o111 to 0o100 leaves it
// passing while a group-executable or other-executable file silently digests as
// non-executable. Two artifact sets that differ only in that would then compare
// equal across the build-to-sign boundary, which is the one thing this digest
// exists to prevent.
func TestDigest_AnyExecuteBitIsTheSameCanonicalIdentity(t *testing.T) {
	t.Parallel()

	executable := map[string]fs.FileMode{
		"owner only": 0o744,
		"group only": 0o654,
		"other only": 0o645,
		"all three":  0o755,
	}

	want := ""

	for name, mode := range executable {
		got := digest(t, fstest.MapFS{"dist/a": {Data: []byte("same"), Mode: mode}})
		if want == "" {
			want = got

			continue
		}

		if got != want {
			t.Errorf("%s (%04o) digested as %s, want the same identity as the others (%s)", name, mode, got, want)
		}
	}

	plain := digest(t, fstest.MapFS{"dist/a": {Data: []byte("same"), Mode: 0o644}})
	if plain == want {
		t.Error("an executable file digested the same as a non-executable one")
	}
}

// TestDigest_NonExecutePermissionsDoNotAffectIt is the other half of the same
// rule. Only the execute bits are canonical, so a file that is 0o600 in one
// checkout and 0o644 in another — umask, a tarball extraction, a CI runner
// image — must not change the artifact identity, or the same build would fail
// its own verification.
func TestDigest_NonExecutePermissionsDoNotAffectIt(t *testing.T) {
	t.Parallel()

	want := ""

	for _, mode := range []fs.FileMode{0o644, 0o600, 0o664, 0o666, 0o400} {
		got := digest(t, fstest.MapFS{"dist/a": {Data: []byte("same"), Mode: mode}})
		if want == "" {
			want = got

			continue
		}

		if got != want {
			t.Errorf("mode %04o digested as %s, want %s: only execute bits are canonical", mode, got, want)
		}
	}
}

// failingFS returns an error from Open for one named path, so the digest's
// fail-closed behaviour can be checked without a real filesystem.
type failingFS struct {
	fs.FS

	failOn string
	err    error
}

func (f failingFS) Open(name string) (fs.File, error) {
	if name == f.failOn {
		return nil, f.err
	}

	return f.FS.Open(name)
}

// TestDigest_FailsClosedOnAReadError proves an unreadable file produces no
// digest at all.
//
// A digest is a claim that the whole tree was seen. Returning a digest computed
// over the files that happened to be readable would be a claim about a
// different tree, and it would compare equal to a later run that also skipped
// the same file — so a permission problem would look like a stable artifact set
// rather than a failure.
func TestDigest_FailsClosedOnAReadError(t *testing.T) {
	t.Parallel()

	base := fstest.MapFS{
		"dist/a": {Data: []byte("one"), Mode: 0o644},
		"dist/b": {Data: []byte("two"), Mode: 0o644},
	}

	fsys := failingFS{FS: base, failOn: "dist/b", err: errUnreadable}

	got, err := domainartifact.Digest(fsys, ".")
	if err == nil {
		t.Fatalf("Digest returned %q for a tree it could not fully read", got)
	}

	if got != "" {
		t.Errorf("Digest returned %q alongside an error; callers that ignore the error would pin a partial tree", got)
	}
}

var errUnreadable = errors.New("artifact test: file cannot be read") //nolint:err113 // test fixture sentinel.
