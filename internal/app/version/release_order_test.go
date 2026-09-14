// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domaingit "github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

// orderedReleaseRepo records every port call in order, including reads, and
// fails the call numbered failAt (1-based) with its own distinct error.
type orderedReleaseRepo struct {
	failAt int
	status string

	localExists, remoteExists bool
	localSHA, remoteSHA       string
	tagType                   string
	localObject, remoteObject string

	calls []string
}

var errReleaseStep = errors.New("release step refused")

func (r *orderedReleaseRepo) CheckOriginPushDestination(context.Context) error {
	return r.step("destination")
}

func (r *orderedReleaseRepo) TagExists(_ context.Context, tag string) (bool, error) {
	return r.localExists, r.step("tag-exists " + tag)
}

func (r *orderedReleaseRepo) TagSHA(_ context.Context, tag string) (string, error) {
	return r.localSHA, r.step("tag-sha " + tag)
}

func (r *orderedReleaseRepo) RemoteTagCommitIfExists(_ context.Context, remote, tag string, _ runcontext.Credential) (string, bool, error) {
	return r.remoteSHA, r.remoteExists, r.step("remote-tag-commit " + remote + " " + tag)
}

func (r *orderedReleaseRepo) RemoteTagObjectIfExists(_ context.Context, remote, tag string, _ runcontext.Credential) (string, bool, error) {
	return r.remoteObject, r.remoteExists, r.step("remote-tag-object " + remote + " " + tag)
}

func (r *orderedReleaseRepo) CatFileType(_ context.Context, ref string) (string, error) {
	return r.tagType, r.step("type " + ref)
}

func (r *orderedReleaseRepo) VerifyConfiguredTagSignature(_ context.Context, tag string) error {
	return r.step("verify " + tag)
}

func (r *orderedReleaseRepo) CreateTag(_ context.Context, tag, ref string, signed bool) error {
	return r.step(fmt.Sprintf("create-tag %s %s signed=%t", tag, ref, signed))
}

func (r *orderedReleaseRepo) PushTagNoForce(_ context.Context, tag string, _ runcontext.Credential) error {
	return r.step("push-tag " + tag)
}

func (r *orderedReleaseRepo) RevParse(_ context.Context, ref string) (string, error) {
	if ref == "HEAD" {
		return headSHA, r.step("rev-parse HEAD")
	}

	return r.localObject, r.step("rev-parse " + ref)
}

func (r *orderedReleaseRepo) Config(_ context.Context, key, _ string) error {
	return r.step("config " + key)
}

func (r *orderedReleaseRepo) SetRemoteURL(_ context.Context, remote, url string) error {
	return r.step("set-url " + remote + " " + url)
}

func (r *orderedReleaseRepo) StatusPorcelain(_ context.Context, pathspec string) (string, error) {
	return r.status, r.step("status " + pathspec)
}

func (r *orderedReleaseRepo) AddPathspecsStrict(_ context.Context, pathspecs []string) error {
	return r.step("add " + strings.Join(pathspecs, " "))
}

func (r *orderedReleaseRepo) Commit(_ context.Context, in domaingit.CommitInput) error {
	return r.step("commit " + in.MessageFile)
}

func (r *orderedReleaseRepo) PushBranchNoForce(_ context.Context, branch string, _ runcontext.Credential) error {
	return r.step("push-branch " + branch)
}

func (r *orderedReleaseRepo) Checkout(_ context.Context, ref string) error {
	return r.step("checkout " + ref)
}

func (r *orderedReleaseRepo) step(call string) error {
	r.calls = append(r.calls, call)
	if len(r.calls) == r.failAt {
		return fmt.Errorf("refused %s: %w", call, errReleaseStep)
	}

	return nil
}

// TestTagRelease_RecoveryOperationsAreOrderedAndOnce records every call of
// each tag-release state: a fresh tag, a tag created locally before an
// interrupted push, an exact tag already on the remote, a remote tag whose
// object differs from the verified local one, a lightweight local tag, and a
// local tag whose signature does not verify. Resolution, type,
// signature, object comparison, creation, push and the release-sha output
// happen in exactly this order and at most once; the refusals stop at the
// check that refused them, with no remote lookup, creation, push or output
// after it.
func TestTagRelease_RecoveryOperationsAreOrderedAndOnce(t *testing.T) {
	t.Parallel()

	const tag = "v1.2.3"

	for name, tc := range map[string]struct {
		repo  orderedReleaseRepo
		calls []string
		want  error
	}{
		"fresh tag": {
			repo:  orderedReleaseRepo{},
			calls: []string{"destination", "rev-parse HEAD", "tag-exists " + tag, "remote-tag-commit origin " + tag, "create-tag " + tag + " " + headSHA + " signed=true", "push-tag " + tag},
		},
		"local tag from an interrupted push": {
			repo: orderedReleaseRepo{localExists: true, localSHA: headSHA, tagType: "tag"},
			calls: []string{"destination", "rev-parse HEAD", "tag-exists " + tag, "tag-sha " + tag, "type refs/tags/" + tag, "verify " + tag,
				"remote-tag-commit origin " + tag, "push-tag " + tag},
		},
		"exact tag already published": {
			repo: orderedReleaseRepo{localExists: true, localSHA: headSHA, tagType: "tag", remoteExists: true, remoteSHA: headSHA, localObject: tagObject, remoteObject: tagObject},
			calls: []string{"destination", "rev-parse HEAD", "tag-exists " + tag, "tag-sha " + tag, "type refs/tags/" + tag, "verify " + tag,
				"remote-tag-commit origin " + tag, "rev-parse refs/tags/" + tag, "remote-tag-object origin " + tag},
		},
		"published tag object differs": {
			repo: orderedReleaseRepo{localExists: true, localSHA: headSHA, tagType: "tag", remoteExists: true, remoteSHA: headSHA, localObject: tagObject, remoteObject: strings.Repeat("9", 40)},
			calls: []string{"destination", "rev-parse HEAD", "tag-exists " + tag, "tag-sha " + tag, "type refs/tags/" + tag, "verify " + tag,
				"remote-tag-commit origin " + tag, "rev-parse refs/tags/" + tag, "remote-tag-object origin " + tag},
			want: errs.ErrValidation,
		},
		"lightweight local tag": {
			repo:  orderedReleaseRepo{localExists: true, localSHA: headSHA, tagType: "commit"},
			calls: []string{"destination", "rev-parse HEAD", "tag-exists " + tag, "tag-sha " + tag, "type refs/tags/" + tag},
			want:  errs.ErrValidation,
		},
		"unverifiable local tag": {
			repo:  orderedReleaseRepo{localExists: true, localSHA: headSHA, tagType: "tag", failAt: 6},
			calls: []string{"destination", "rev-parse HEAD", "tag-exists " + tag, "tag-sha " + tag, "type refs/tags/" + tag, "verify " + tag},
			want:  errs.ErrPermissionDenied,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			repo := tc.repo
			sink := fakeoutputsink.New(t)

			var out bytes.Buffer

			_, err := appversion.TagRelease(t.Context(), &repo, appversion.TagReleaseInput{Tag: tag, Signed: true}, sink, &out)
			require.Equal(t, tc.calls, repo.calls)

			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, sink.Keys())
				require.Empty(t, out.String())

				return
			}

			require.NoError(t, err)
			require.Equal(t, map[string]string{"release-sha": headSHA}, sink.AllScalar())
		})
	}
}

// TestTagRelease_EachFailingCallStopsTheFreshRelease fails each call of the
// fresh-tag sequence in turn: the error keeps that call's cause, the trace
// ends at it, and nothing is emitted.
func TestTagRelease_EachFailingCallStopsTheFreshRelease(t *testing.T) {
	t.Parallel()

	for failAt := 1; failAt <= 6; failAt++ {
		repo := &orderedReleaseRepo{failAt: failAt}
		sink := fakeoutputsink.New(t)

		_, err := appversion.TagRelease(t.Context(), repo, appversion.TagReleaseInput{Tag: "v1.2.3", Signed: true}, sink, &bytes.Buffer{})
		require.ErrorIs(t, err, errReleaseStep, "call %d", failAt)
		require.Len(t, repo.calls, failAt)
		require.Empty(t, sink.Keys(), "call %d", failAt)
	}
}

// TestChangelogRelease_OperationsAreOrderedAndOnce records the whole signed
// changelog release: destination and changelog checks, both tag-absence
// checks, signing and remote setup, the repeated destination check, stage,
// commit, branch push, then the tag release and the final checkout, each
// mutation exactly once. Failing each call in turn ends the trace at it with
// its own cause and no release-sha output.
func TestChangelogRelease_OperationsAreOrderedAndOnce(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "CHANGELOG.md", "# v1.2.3\n")
	writeFile(t, dir, "commit-msg.txt", "chore(release): bump to v1.2.3\n")
	t.Chdir(dir)

	const url = "ssh://git@codeberg.org:2222/itiquette/example.git"

	in := appversion.ChangelogReleaseInput{
		Tag: "v1.2.3", Repository: "itiquette/example", GitURL: url,
		AuthorName: "Release Bot", AuthorEmail: "bot@example.invalid", SigningKeyPath: "/tmp/key", TagSigned: true,
	}

	repo := &orderedReleaseRepo{status: " M CHANGELOG.md"}
	sink := fakeoutputsink.New(t)
	_, err := appversion.ChangelogRelease(t.Context(), repo, sink, &bytes.Buffer{}, in)
	require.NoError(t, err)

	require.Equal(t, []string{
		"destination", "status CHANGELOG.md", "remote-tag-commit " + url + " v1.2.3", "tag-exists v1.2.3",
		"config user.name", "config user.email", "config gpg.format", "config user.signingkey", "config commit.gpgsign",
		"set-url origin " + url, "destination",
		"add CHANGELOG.md", "commit commit-msg.txt", "push-branch main",
		"destination", "rev-parse HEAD", "tag-exists v1.2.3", "remote-tag-commit origin v1.2.3",
		"create-tag v1.2.3 " + headSHA + " signed=true", "push-tag v1.2.3", "checkout v1.2.3",
	}, repo.calls)
	require.Equal(t, map[string]string{"release-sha": headSHA}, sink.AllScalar())

	success := repo.calls
	for failAt := 1; failAt <= len(success); failAt++ {
		failing := &orderedReleaseRepo{status: " M CHANGELOG.md", failAt: failAt}
		failingSink := fakeoutputsink.New(t)

		_, err := appversion.ChangelogRelease(t.Context(), failing, failingSink, &bytes.Buffer{}, in)
		require.ErrorIs(t, err, errReleaseStep, "failing %s", success[failAt-1])
		require.Equal(t, success[:failAt], failing.calls, "failing %s", success[failAt-1])

		if failAt < len(success) {
			require.Empty(t, failingSink.Keys(), "failing %s", success[failAt-1])
		}
	}
}
