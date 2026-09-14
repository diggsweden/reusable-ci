// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package platform_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	appplatform "github.com/diggsweden/reusable-ci/v3/internal/app/platform"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

// ResolveRef turns whatever `git ls-remote` printed into the commit every later
// stage builds, signs and publishes against. The parser is the whole security
// boundary: it is the only thing standing between a remote's output and a
// checkout, and a remote can put more than one row in front of it.
//
// The existing tests cover the happy paths one at a time. This table states the
// grammar as a closed set, so the interesting half — every shape that must be
// REFUSED — is written down rather than assumed. A parser that silently took
// the first row, or the last, or preferred whichever came back, would pass all
// of the happy-path tests and none of these.
//
// SHA-1 and SHA-256 appear throughout because the resolver must not accept a
// remote that answers in both widths at once: mixed widths mean two object
// databases, and picking either one is a guess.

const (
	sha1A   = "1111111111111111111111111111111111111111"
	sha1B   = "2222222222222222222222222222222222222222"
	sha256A = "3333333333333333333333333333333333333333333333333333333333333333"
	sha256B = "4444444444444444444444444444444444444444444444444444444444444444"
)

func TestResolveRef_TheLSRemoteGrammarIsAClosedSet(t *testing.T) {
	t.Parallel()

	rows := func(pairs ...string) string { return strings.Join(pairs, "\n") }

	for _, tc := range []struct {
		name string
		ref  string
		out  string
		want string // "" means the resolution must be refused
		why  string
	}{
		{
			name: "branch", ref: "main", out: sha1A + "\trefs/heads/main", want: sha1A,
			why: "a short name that only exists as a branch resolves to it",
		},
		{
			name: "full branch ref", ref: "refs/heads/main", out: sha1A + "\trefs/heads/main", want: sha1A,
			why: "a fully qualified name is looked up verbatim, not re-prefixed",
		},
		{
			name: "lightweight tag", ref: "v1", out: sha1A + "\trefs/tags/v1", want: sha1A,
			why: "an unannotated tag has no peeled row and resolves to the row it has",
		},
		{
			name: "annotated tag prefers the peeled commit", ref: "v1",
			out: rows(sha1A+"\trefs/tags/v1", sha1B+"\trefs/tags/v1^{}"), want: sha1B,
			why: "the tag object is not a commit; the checkout needs the commit it points at",
		},
		{
			name: "peeled row without its tag", ref: "v1", out: sha1B + "\trefs/tags/v1^{}", want: "",
			why: "a peel with nothing to peel from is unbound: there is no evidence the tag itself exists",
		},
		{
			name: "HEAD", ref: "HEAD", out: sha1A + "\tHEAD", want: sha1A,
			why: "HEAD is neither a branch nor a tag and must not be re-prefixed into one",
		},
		{
			name: "branch and tag of the same name", ref: "release",
			out: rows(sha1A+"\trefs/heads/release", sha1B+"\trefs/tags/release"), want: "",
			why: "ambiguous: choosing either one is a guess about what the operator meant",
		},
		{
			name: "branch and tag of the same name at the same commit", ref: "release",
			out: rows(sha1A+"\trefs/heads/release", sha1A+"\trefs/tags/release"), want: "",
			why: "still ambiguous; agreeing today does not make the reference unambiguous tomorrow",
		},
		{
			name: "an unrequested ref in the answer", ref: "main",
			out: rows(sha1A+"\trefs/heads/main", sha1B+"\trefs/heads/other"), want: "",
			why: "a remote that answers more than it was asked is not a remote to take the first row from",
		},
		{
			name: "row without a tab", ref: "main", out: sha1A + " refs/heads/main", want: "",
			why: "ls-remote is tab separated; a space-separated row is not that format",
		},
		{
			name: "row with an extra field", ref: "main", out: sha1A + "\trefs/heads/main\textra", want: "",
			why: "three fields is not the grammar, and the third could be anything",
		},
		{
			name: "row whose first field is not an object ID", ref: "main", out: "not-a-sha\trefs/heads/main", want: "",
			why: "the value would be handed to a checkout verbatim",
		},
		{
			name: "short object ID in the row", ref: "main", out: sha1A[:12] + "\trefs/heads/main", want: "",
			why: "abbreviated IDs are ambiguous by construction",
		},
		{
			name: "identical duplicate rows", ref: "main",
			out: rows(sha1A+"\trefs/heads/main", sha1A+"\trefs/heads/main"), want: sha1A,
			why: "a repeated row carries no conflicting information",
		},
		{
			name: "duplicate rows that disagree", ref: "main",
			out: rows(sha1A+"\trefs/heads/main", sha1B+"\trefs/heads/main"), want: "",
			why: "the same name at two commits in one answer is a remote to refuse, not to pick from",
		},
		{
			name: "mixed object ID widths", ref: "v1",
			out: rows(sha1A+"\trefs/tags/v1", sha256B+"\trefs/tags/v1^{}"), want: "",
			why: "two widths means two object databases; neither can be verified against the other",
		},
		{
			name: "sha-256 branch", ref: "main", out: sha256A + "\trefs/heads/main", want: sha256A,
			why: "a sha-256 remote is supported, in full width",
		},
		{
			name: "sha-256 annotated tag", ref: "v1",
			out: rows(sha256A+"\trefs/tags/v1", sha256B+"\trefs/tags/v1^{}"), want: sha256B,
			why: "peel binding is width independent",
		},
		{
			name: "sha-1 object ID with no rows", ref: sha1A, out: "", want: sha1A,
			why: "a full object ID the remote does not advertise is still a valid checkout target",
		},
		{
			name: "sha-256 object ID with no rows", ref: sha256A, out: "", want: sha256A,
			why: "the pass-through is width independent too",
		},
		{
			name: "object ID answered with an unrelated row", ref: sha1A, out: sha1B + "\trefs/heads/main", want: "",
			why: "pass-through is for silence; a row that does not name the request is not silence",
		},
		{
			name: "no rows for a named ref", ref: "main", out: "", want: "",
			why: "absent is absent; there is nothing to fall back to",
		},
		{
			name: "trailing newline", ref: "main", out: sha1A + "\trefs/heads/main\n", want: sha1A,
			why: "git terminates its last row; that is not a malformed empty row",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := appplatform.ResolveRef(context.Background(), fakeRefGit{out: tc.out},
				fakeoutputsink.New(t), io.Discard,
				appplatform.ResolveRefInput{RemoteURL: "https://example.com/o/r", Ref: tc.ref})

			if tc.want == "" {
				if err == nil {
					t.Fatalf("resolved %q, want a refusal: %s", got, tc.why)
				}

				return
			}

			if err != nil {
				t.Fatalf("%v: %s", err, tc.why)
			}

			if got != tc.want {
				t.Errorf("got %q, want %q: %s", got, tc.want, tc.why)
			}
		})
	}
}

// Globs, ranges, peel suffixes and abbreviated IDs are all refused. Two
// independent layers do it: the input guard, before the remote is contacted at
// all, and the name binding in the parser afterwards.
//
// A fault replay is worth recording here. Removing the input guard alone does
// NOT make this test fail: the parser still refuses, because it will not bind a
// returned row to a name that was never requested. So what this pins is the
// behaviour, which is the contract, and the two layers are genuinely redundant
// rather than one guard with a test each. The guard earns its place by refusing
// before the query rather than after it.
func TestResolveRef_RefusesPatternsAndOtherNonLiteralRefs(t *testing.T) {
	t.Parallel()

	for _, ref := range []string{
		"refs/heads/*", "refs/tags/v1.*", "*", "main..other", "main^{}", "HEAD~1",
		"-refs/heads/main", "refs/heads/ma in", "refs/heads/main\nrefs/heads/other",
		sha1A[:12],
	} {
		t.Run(strings.ReplaceAll(ref, "\n", "\\n"), func(t *testing.T) {
			t.Parallel()

			// A well-formed row for a real branch, so the refusal cannot come
			// from the answer being unparsable.
			_, err := appplatform.ResolveRef(context.Background(),
				fakeRefGit{out: sha1A + "\trefs/heads/main"}, fakeoutputsink.New(t), io.Discard,
				appplatform.ResolveRefInput{RemoteURL: "https://example.com/o/r", Ref: ref})
			if err == nil {
				t.Fatalf("ref %q was accepted; only literal reference names and full object IDs are supported", ref)
			}

			if !errors.Is(err, errs.ErrValidation) && !errors.Is(err, errs.ErrUsage) && !errors.Is(err, errs.ErrInvalidConfig) {
				t.Errorf("ref %q: err = %v, want a classified refusal", ref, err)
			}
		})
	}
}
