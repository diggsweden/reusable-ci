// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domaingit "github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// These tests inspect the production builders only; they never run a process,
// open a repository, access config, sign an object, or contact a destination.
func TestPublicationBinding_LeasedBranchCommand(t *testing.T) {
	t.Parallel()

	for _, size := range []int{40, 64} {
		in := domaingit.BranchPushInput{CommitSHA: strings.Repeat("c", size), ExpectedSHA: strings.Repeat("a", size), Branch: "release/1.x"}
		args, err := leasedBranchPushArgs(in)
		require.NoError(t, err)
		require.Equal(t, []string{"--no-replace-objects", "-c", "core.hooksPath=/dev/null", "-c", "remote.origin.mirror=false", "push",
			"--no-follow-tags", "--recurse-submodules=no", "--force-with-lease=refs/heads/release/1.x:" + in.ExpectedSHA,
			"--", "origin", in.CommitSHA + "^{commit}:refs/heads/release/1.x"}, args)
		// A SHA-shaped branch is legal, but the source must never be matched
		// against a same-named local branch/tag by push's refspec resolver.
		in.Branch = in.CommitSHA
		args, err = leasedBranchPushArgs(in)
		require.NoError(t, err)
		require.Equal(t, in.CommitSHA+"^{commit}:refs/heads/"+in.Branch, args[len(args)-1])
	}
}

func TestPublicationBinding_RejectsUnboundPushRequests(t *testing.T) {
	t.Parallel()

	valid := domaingit.BranchPushInput{CommitSHA: strings.Repeat("c", 40), ExpectedSHA: strings.Repeat("a", 40), Branch: "main"}
	for _, bad := range []string{"", "HEAD", "--all", strings.Repeat("A", 40), strings.Repeat("b", 39)} {
		in := valid
		in.CommitSHA = bad
		args, err := leasedBranchPushArgs(in)
		require.ErrorIs(t, err, errs.ErrUsage)
		require.Nil(t, args)

		in = valid
		in.ExpectedSHA = bad
		args, err = leasedBranchPushArgs(in)
		require.ErrorIs(t, err, errs.ErrUsage)
		require.Nil(t, args)
	}

	for _, branch := range []string{"", "main:refs/tags/v1.0.0", "main*", "main\nother", "../main"} {
		in := valid
		in.Branch = branch
		args, err := leasedBranchPushArgs(in)
		require.ErrorIs(t, err, errs.ErrUsage)
		require.Nil(t, args)
	}

	for _, sha := range []string{valid.ExpectedSHA, strings.Repeat("c", 64)} {
		in := valid
		in.CommitSHA = sha
		_, err := leasedBranchPushArgs(in)
		require.ErrorIs(t, err, errs.ErrUsage)
	}
}

func TestPublicationBinding_NativeDestinationInspection(t *testing.T) {
	t.Parallel()
	// Git, not a Go config parser, resolves includes, multiple URLs and rewrites.
	require.Equal(t, []string{"remote", "get-url", "--all", "origin"}, originURLArgs(false))
	require.Equal(t, []string{"remote", "get-url", "--all", "--push", "origin"}, originURLArgs(true))

	for _, url := range []string{"https://forge.example/owner/repo.git", "ssh://git@forge.example:2222/owner/repo.git", "git@forge.example:owner/repo.git", "/local/repo.git"} {
		require.NoError(t, validateOriginURLs(url, url))
		require.NoError(t, validateOriginURLs(url+"\n", url+"\n"))
	}

	url := "https://user:fixture-secret@forge.example/owner/repo.git" //nolint:gosec // Synthetic credential marker proves refusal errors do not echo URLs.
	for _, pair := range [][2]string{
		{url, "https://other.example/owner/repo.git"},
		{url, url + "\n" + url},
		{url + "\n" + url, url + "\n" + url},
		{"", ""}, {url + "\r", url + "\r"}, {" " + url, " " + url},
		{url + "\x00", url + "\x00"},
	} {
		err := validateOriginURLs(pair[0], pair[1])
		require.ErrorIs(t, err, errs.ErrValidation)
		require.NotContains(t, err.Error(), "fixture-secret")
		require.NotContains(t, err.Error(), "forge.example")
	}
}

func TestPublicationBinding_RawParentInspection(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"--no-replace-objects", "-c", "core.hooksPath=/dev/null", "rev-parse", "--verify", "HEAD^{commit}"}, headCommitArgs())

	for _, size := range []int{40, 64} {
		sha, parent := strings.Repeat("c", size), strings.Repeat("a", size)
		args, err := commitParentsArgs(sha)
		require.NoError(t, err)
		require.Equal(t, []string{"--no-replace-objects", "-c", "core.hooksPath=/dev/null", "cat-file", "commit", sha}, args)

		for _, headers := range []string{"", "parent " + parent + "\n", "parent " + parent + "\nparent " + sha + "\n"} {
			body := "tree " + sha + "\n" + headers + "author Fixture <fixture@example.invalid> 0 +0000\ngpgsig header\n parent ignored-continuation\n\nparent ignored-message\n"
			parents, err := commitParentsFromObject(body)
			require.NoError(t, err)

			var want []string
			if headers != "" {
				want = append(want, parent)
				if strings.Count(headers, "parent ") == 2 {
					want = append(want, sha)
				}
			}

			require.Equal(t, want, parents)
		}
	}

	_, err := commitParentsArgs("HEAD")
	require.ErrorIs(t, err, errs.ErrUsage)

	for _, body := range []string{"parent " + strings.Repeat("a", 40), "parent HEAD\n\nbody", "parent " + strings.Repeat("a", 40) + " extra\n\nbody"} {
		_, err := commitParentsFromObject(body)
		require.ErrorIs(t, err, errs.ErrValidation)
	}
}

func TestPublicationBinding_CreateAndPushExactTag(t *testing.T) {
	t.Parallel()

	sha := strings.Repeat("c", 40)
	sha256 := strings.Repeat("d", 64)

	for _, signed := range []bool{false, true} {
		mode := "-a"
		if signed {
			mode = "-s"
		}

		for _, tc := range []struct{ ref, want string }{
			{sha, sha + "^{commit}"},
			{sha256, sha256 + "^{commit}"},
			{"", ""},
			{"refs/tags/base", "refs/tags/base"},
			{"refs/heads/main", "refs/heads/main"},
			{"HEAD~2^{commit}", "HEAD~2^{commit}"},
			{sha + "^{commit}", sha + "^{commit}"},
			{"abcdef0", "abcdef0"},
			{strings.ToUpper(sha), strings.ToUpper(sha)},
		} {
			want := []string{"--no-replace-objects", "-c", "core.hooksPath=/dev/null", "tag", mode, "v1.2.3", "-m", "v1.2.3"}
			if tc.want != "" {
				want = append(want, tc.want)
			}

			require.Equal(t, want, createTagArgs("v1.2.3", tc.ref, signed), "ref=%q signed=%v", tc.ref, signed)
		}
	}

	args, err := tagPushArgs("v1.2.3")
	require.NoError(t, err)
	require.Equal(t, []string{"--no-replace-objects", "-c", "core.hooksPath=/dev/null", "-c", "remote.origin.mirror=false", "push",
		"--no-follow-tags", "--recurse-submodules=no", "--", "origin", "refs/tags/v1.2.3:refs/tags/v1.2.3"}, args)

	_, err = tagPushArgs("v1.2.3:refs/heads/main")
	require.ErrorIs(t, err, errs.ErrUsage)
}

func TestPublicationBinding_RepositoryArgumentAndAudienceStaySeparate(t *testing.T) {
	t.Parallel()

	for _, repository := range []string{"origin", "ssh://git@forge.example:2222/o/r.git", "git@forge.example:o/r.git", "https://entry.example/o/r.git", "/local/repo.git", "../local repo.git"} {
		require.Equal(t, []string{"-c", "core.hooksPath=/dev/null", "ls-remote", "--get-url", "--", repository}, repositoryURLArgs(repository))

		for _, peel := range []bool{false, true} {
			want := []string{"ls-remote", "--tags", "--", repository}
			if peel {
				want = append(want, "refs/tags/v1.2.3^{}")
			}

			want = append(want, "refs/tags/v1.2.3")
			require.Equal(t, want, remoteTagQueryArgs(repository, "v1.2.3", peel))
		}
	}
	// A native rewrite may resolve entry -> audience, while a different rule
	// would rewrite audience -> elsewhere if it were replayed as an argument.
	const (
		original  = "https://entry.example/o/r.git"
		effective = "https://audience.example/o/r.git"
	)

	env, err := appendRemoteAuthConfig(authEnv(effective, runcontext.OperatorCredential("fixture-token")), "2")
	require.NoError(t, err)
	require.Contains(t, env, "GIT_CONFIG_KEY_2=http."+effective+".extraheader")

	args := remoteTagQueryArgs(original, "v1.2.3", true)
	require.Contains(t, args, original)
	require.NotContains(t, args, effective)
	require.NotContains(t, strings.Join(args, " "), "fixture-token")
}

func TestPublicationBinding_RefusesRepositoryRoutingOverrides(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_SHALLOW_FILE", "GIT_GRAFT_FILE", "GIT_REPLACE_REF_BASE", "GIT_CEILING_DIRECTORIES", "GIT_DISCOVERY_ACROSS_FILESYSTEM"} {
		for _, value := range []string{"", "/fixture-sensitive-route"} {
			err := validateRepositoryRouting([]string{key + "=" + value})
			require.ErrorIs(t, err, errs.ErrValidation, key)
			require.NotContains(t, err.Error(), "fixture-sensitive-route")
		}
	}

	require.NoError(t, validateRepositoryRouting([]string{
		"GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=remote.origin.url", "GIT_CONFIG_VALUE_0=https://entry.example/o/r.git",
		"GIT_CONFIG_KEY_1=url.https://audience.example/.insteadOf", "GIT_CONFIG_VALUE_1=https://entry.example/",
		"GIT_TERMINAL_PROMPT=0",
	}))
}

func TestPublicationBinding_BranchPushIsLiteralAndNoForce(t *testing.T) {
	t.Parallel()

	for _, branch := range []string{"main", "release/1.x", strings.Repeat("a", 40)} {
		args, err := branchPushArgs(branch)
		require.NoError(t, err)

		ref := "refs/heads/" + branch
		require.Equal(t, []string{"--no-replace-objects", "-c", "core.hooksPath=/dev/null", "-c", "remote.origin.mirror=false", "push",
			"--no-follow-tags", "--recurse-submodules=no", "--", "origin", ref + ":" + ref}, args)
	}

	for _, branch := range []string{"", "--mirror", "--all", "+main", "+refs/heads/main:refs/heads/main", "main:other", "main*", "main\nother", "../main"} {
		args, err := branchPushArgs(branch)
		require.ErrorIs(t, err, errs.ErrUsage, branch)
		require.Nil(t, args)
	}
}

func TestPublicationBinding_AuthPreservesInheritedGitConfig(t *testing.T) {
	t.Parallel()

	url := "https://forge.example/owner/repo.git"
	base := authEnv(url, runcontext.OperatorCredential("fixture-token"))
	env, err := appendRemoteAuthConfig(base, "2")
	require.NoError(t, err)
	require.Equal(t, []string{"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT=3", "GIT_CONFIG_KEY_2=http." + url + ".extraheader",
		strings.Replace(base[3], "GIT_CONFIG_VALUE_0=", "GIT_CONFIG_VALUE_2=", 1)}, env)

	for _, count := range []string{"", "0"} {
		env, err = appendRemoteAuthConfig(base, count)
		require.NoError(t, err)
		require.Equal(t, base, env)
	}

	for _, count := range []string{"-1", "not-a-count", "2147483647", "4294967296"} {
		env, err = appendRemoteAuthConfig(base, count)
		require.ErrorIs(t, err, errs.ErrValidation)
		require.Nil(t, env)
	}

	noAuth := authEnv(url, runcontext.Credential{})
	env, err = appendRemoteAuthConfig(noAuth, "2")
	require.NoError(t, err)
	require.Equal(t, []string{"GIT_TERMINAL_PROMPT=0"}, env)
}
