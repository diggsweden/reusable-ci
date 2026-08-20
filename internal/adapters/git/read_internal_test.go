// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// remoteTagCommitFromOutput decides which commit a published tag names.
// The signer compares that answer against the release SHA and refuses to
// publish when they differ, so getting it wrong in either direction is a
// trust-boundary failure: the wrong commit gets signed, or a correct
// release is refused.
//
// The one case that matters most is an annotated tag. `git ls-remote`
// then reports two lines -- the tag object, and the commit it peels to --
// and only the second is the release commit. Returning the tag object's
// sha would compare a tag object against a commit sha and refuse every
// annotated release.
func TestRemoteTagCommitFromOutput(t *testing.T) {
	t.Parallel()

	const (
		tagObject = "1111111111111111111111111111111111111111"
		commit    = "2222222222222222222222222222222222222222"
	)

	for _, tc := range []struct {
		name string
		out  string
		want string
	}{
		{
			name: "annotated tag peels to its commit",
			out: tagObject + "\trefs/tags/v1.2.3\n" +
				commit + "\trefs/tags/v1.2.3^{}\n",
			want: commit,
		},
		{
			// Order is the remote's choice, not ours.
			name: "annotated tag with the peeled line first",
			out: commit + "\trefs/tags/v1.2.3^{}\n" +
				tagObject + "\trefs/tags/v1.2.3\n",
			want: commit,
		},
		{
			// A lightweight tag is already a commit and has no peeled line.
			name: "lightweight tag",
			out:  commit + "\trefs/tags/v1.2.3\n",
			want: commit,
		},
		{
			name: "trailing newline and blank lines",
			out:  "\n" + commit + "\trefs/tags/v1.2.3\n\n",
			want: commit,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := remoteTagCommitFromOutput(tc.out, "https://forge/repo.git", "v1.2.3")
			if err != nil {
				t.Fatal(err)
			}

			if got != tc.want {
				t.Errorf("commit = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRemoteTagCommitFromOutput_NotFound covers the answer that has to be
// distinguishable: RemoteTagCommitIfExists turns exactly ErrValidation
// into exists=false, and anything else into a hard error. A "not found"
// that arrived as some other error would abort a release that should
// simply have proceeded without recovery.
func TestRemoteTagCommitFromOutput_NotFound(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		out  string
	}{
		{name: "no output at all", out: ""},
		{name: "whitespace only", out: "  \n\t\n"},
		{name: "lines with no ref column", out: "2222222222222222222222222222222222222222\n"},
		{
			name: "a different tag entirely",
			out:  "2222222222222222222222222222222222222222\trefs/tags/v9.9.9\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := remoteTagCommitFromOutput(tc.out, "https://forge/repo.git", "v1.2.3")
			if !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("got (%q, %v), want ErrValidation", got, err)
			}
		})
	}
}

// TestRemoteTagCommitFromOutput_PeeledMatchIsNotNameChecked records an
// asymmetry rather than endorsing it. The plain branch requires the ref
// to be exactly refs/tags/<tag>; the peeled branch accepts any ref ending
// in "^{}" and takes the last one it sees.
//
// It is unreachable today: `git ls-remote` is asked for the two exact
// refspecs, and both callers gate the tag through IsStableSemverTag
// first, so no glob metacharacter can turn those refspecs into patterns
// that match a second tag. This test exists so the gap is visible if a
// third caller ever arrives without that gate -- it would resolve one
// tag's name to another tag's commit, and the signer would sign it.
//
// Recorded in docs/open-questions.md ("The peeled branch of the remote
// tag parser does not check the ref name").
func TestRemoteTagCommitFromOutput_PeeledMatchIsNotNameChecked(t *testing.T) {
	t.Parallel()

	const wrongCommit = "3333333333333333333333333333333333333333"

	out := wrongCommit + "\trefs/tags/v9.9.9^{}\n" +
		"2222222222222222222222222222222222222222\trefs/tags/v1.2.3\n"

	got, err := remoteTagCommitFromOutput(out, "https://forge/repo.git", "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}

	if got != wrongCommit {
		t.Fatalf("the peeled branch now checks the ref name (got %q) -- close the open question and delete this test", got)
	}
}
