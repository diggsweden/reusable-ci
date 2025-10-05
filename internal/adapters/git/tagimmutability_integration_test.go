//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// A release tag is created once and never moved. The existing test recreates it
// at the SAME commit and asserts an error, which leaves the interesting half
// unchecked: whether the tag SURVIVED. A CreateTag that deleted the old ref and
// then failed to write the new one would satisfy an error-only assertion while
// destroying the tag a release was published under.
//
// The retry here is against a different commit, which is the shape that
// matters. That is what a rerun after a moved HEAD looks like, and it is the
// case where "the tag is never moved" has to mean the object is byte-identical
// afterwards, not merely that the second call complained.
func TestCreateTag_ARejectedRecreationLeavesTheOriginalTagIntact(t *testing.T) {
	repo, isolated := newRepo(t)
	ctx := context.Background()

	original := isolated.HeadSHA()
	require.NoError(t, repo.CreateTag(ctx, "v1.2.3", "HEAD", false))

	// Everything about the tag as it exists now: the tag object itself, the
	// commit it peels to, and its message.
	tagObject := isolated.Git("rev-parse", "refs/tags/v1.2.3")
	peeled := isolated.Git("rev-parse", "refs/tags/v1.2.3^{commit}")
	message := isolated.Git("tag", "-l", "--format=%(contents)", "v1.2.3")

	require.Equal(t, original, peeled, "the tag must peel to the commit it was created at")

	// Move HEAD on, so the retry genuinely names something else.
	moved := isolated.AddCommit("chore: work continued after the tag")
	require.NotEqual(t, original, moved)

	require.Error(t, repo.CreateTag(ctx, "v1.2.3", "HEAD", false),
		"recreating an existing tag at another commit must fail")

	require.Equal(t, tagObject, isolated.Git("rev-parse", "refs/tags/v1.2.3"),
		"the tag object changed; a release published under this tag now resolves to something else")
	require.Equal(t, peeled, isolated.Git("rev-parse", "refs/tags/v1.2.3^{commit}"),
		"the tag peels to a different commit than it was created at")
	require.Equal(t, message, isolated.Git("tag", "-l", "--format=%(contents)", "v1.2.3"),
		"the tag message changed, so the object was rewritten even though the ref looks the same")
	require.Equal(t, "tag", isolated.Git("cat-file", "-t", "refs/tags/v1.2.3"),
		"the annotated tag became something else")
}

// The same, one level up: an explicit ref rather than HEAD. A rerun that passes
// the new commit by name is the more likely spelling in a workflow, and it must
// be refused just as firmly.
func TestCreateTag_ARejectedRecreationAtANamedCommitLeavesTheTagIntact(t *testing.T) {
	repo, isolated := newRepo(t)
	ctx := context.Background()

	require.NoError(t, repo.CreateTag(ctx, "v2.0.0", "HEAD", false))

	tagObject := isolated.Git("rev-parse", "refs/tags/v2.0.0")
	other := isolated.AddCommit("chore: another commit")

	require.Error(t, repo.CreateTag(ctx, "v2.0.0", other, false))
	require.Equal(t, tagObject, isolated.Git("rev-parse", "refs/tags/v2.0.0"))
}
