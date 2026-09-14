// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git

import (
	"errors"
	"strings"
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
func TestRemoteTagCommitFromOutput_PeelsAnnotatedTagsInAnyOrder(t *testing.T) {
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

func TestRemoteTagCommitFromOutput_IgnoresAnotherTagsPeeledRef(t *testing.T) {
	t.Parallel()

	const wrongCommit = "3333333333333333333333333333333333333333"

	out := wrongCommit + "\trefs/tags/v9.9.9^{}\n" +
		"2222222222222222222222222222222222222222\trefs/tags/v1.2.3\n"

	const expected = "2222222222222222222222222222222222222222"

	got, err := remoteTagCommitFromOutput(out, "https://forge/repo.git", "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}

	if got != expected {
		t.Fatalf("got %q, want the requested tag object %q", got, expected)
	}
}

// TestRemoteTagParsers_RefuseAnswersWithNoRightReading covers the two kinds of
// remote output the parsers used to accept silently.
//
// A column-one value that is not a full object hash was returned as the
// answer, including one shaped like a git option. A ref reported twice with
// different IDs resolved to whichever line the scan kept -- the last for the
// commit reader, the first for the tag-object reader -- so one response could
// name two different objects depending on which function asked.
//
// Each must be refused as ErrMalformedInput and, specifically, NOT as
// ErrValidation: the *IfExists callers read ErrValidation as "the tag is not
// there", and treating a nonsensical answer as absence would let a release go
// on to create a tag the remote may already hold.
func TestRemoteTagParsers_RefuseAnswersWithNoRightReading(t *testing.T) {
	t.Parallel()

	const (
		first  = "2222222222222222222222222222222222222222"
		second = "3333333333333333333333333333333333333333"
	)

	for name, out := range map[string]string{
		"a malformed id":                    "not-a-sha\trefs/tags/v1.2.3\n",
		"an option-shaped id":               "--upload-pack=evil\trefs/tags/v1.2.3\n",
		"an abbreviated id":                 "2222222\trefs/tags/v1.2.3\n",
		"an uppercase id":                   strings.ToUpper("abcdef") + first[6:] + "\trefs/tags/v1.2.3\n",
		"the tag twice with different ids":  first + "\trefs/tags/v1.2.3\n" + second + "\trefs/tags/v1.2.3\n",
		"the peeled ref twice, differently": first + "\trefs/tags/v1.2.3^{}\n" + second + "\trefs/tags/v1.2.3^{}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for reader, parse := range map[string]func(string, string, string) (string, error){
				"commit": remoteTagCommitFromOutput,
				"object": remoteTagObjectFromOutput,
			} {
				got, err := parse(out, "https://forge/repo.git", "v1.2.3")

				// The object reader never looks at the peeled ref, so a
				// conflict there is invisible to it and is simply "not found".
				if reader == "object" && strings.Contains(name, "peeled") {
					if !errors.Is(err, errs.ErrValidation) {
						t.Errorf("%s: got (%q, %v), want not-found for a ref it does not read", reader, got, err)
					}

					continue
				}

				if !errors.Is(err, errs.ErrMalformedInput) {
					t.Errorf("%s: got (%q, %v), want ErrMalformedInput", reader, got, err)
				}

				if errors.Is(err, errs.ErrValidation) {
					t.Errorf("%s: a malformed answer would be read as an absent tag: %v", reader, err)
				}

				if got != "" {
					t.Errorf("%s: a refused answer still returned %q", reader, got)
				}
			}
		})
	}
}

// TestRemoteTagParsers_AcceptWhatARealRemoteSends is the positive side of the
// refusal above: repeated identical lines, which a remote may legitimately
// send, stay accepted, and a SHA-256 repository's 64-character IDs are object
// IDs like any other.
func TestRemoteTagParsers_AcceptWhatARealRemoteSends(t *testing.T) {
	t.Parallel()

	sha256ID := strings.Repeat("ab", 32)
	sha1ID := strings.Repeat("c", 40)

	for name, tc := range map[string]struct{ out, want string }{
		"a SHA-256 lightweight tag":     {out: sha256ID + "\trefs/tags/v1.2.3\n", want: sha256ID},
		"an identical duplicate line":   {out: sha1ID + "\trefs/tags/v1.2.3\n" + sha1ID + "\trefs/tags/v1.2.3\n", want: sha1ID},
		"a malformed id on another tag": {out: "garbage\trefs/tags/v9.9.9\n" + sha1ID + "\trefs/tags/v1.2.3\n", want: sha1ID},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for reader, parse := range map[string]func(string, string, string) (string, error){
				"commit": remoteTagCommitFromOutput,
				"object": remoteTagObjectFromOutput,
			} {
				got, err := parse(tc.out, "https://forge/repo.git", "v1.2.3")
				if err != nil || got != tc.want {
					t.Errorf("%s: got (%q, %v), want %q", reader, got, err, tc.want)
				}
			}
		})
	}
}
