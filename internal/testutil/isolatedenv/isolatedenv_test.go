// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package isolatedenv_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/isolatedenv"
)

func TestIsolate_UnsetsGitRoutingAndRestoresCallerEnvironment(t *testing.T) {
	keys := []string{"GIT_DIR", "GIT_COMMON_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_CEILING_DIRECTORIES", "GIT_DISCOVERY_ACROSS_FILESYSTEM", "GIT_CONFIG", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0", "GIT_TRACE", "GIT_TRACE2_EVENT"}

	value := t.TempDir()
	for _, key := range keys {
		t.Setenv(key, value)
	}

	t.Run("isolated", func(t *testing.T) {
		isolatedenv.Isolate(t)

		for _, key := range keys {
			_, present := os.LookupEnv(key)
			require.False(t, present, "%s must be unset, not merely empty", key)
		}

		require.Equal(t, "1", os.Getenv("GIT_CONFIG_NOSYSTEM"))
		require.Equal(t, "false", os.Getenv("GIT_SSH_COMMAND"))
	})

	for _, key := range keys {
		require.Equal(t, value, os.Getenv(key), "%s was not restored", key)
	}
}

func TestIsolate_ScrubsAndPinsProcessEnv(t *testing.T) {
	home := isolatedenv.Isolate(t)
	require.Equal(t, home, os.Getenv("HOME"))
	require.Equal(t, home, os.Getenv("USERPROFILE"))
	// All three temp names: TMPDIR is Unix's, TMP and TEMP are Windows',
	// and Node and Python fall back to them. One of them still pointing
	// at the host's /tmp makes the other two decorative.
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		requireBelow(t, home, key)
	}

	requireBelow(t, home, "GNUPGHOME")
	requireBelow(t, home, "GIT_TEMPLATE_DIR")

	require.Equal(t, "1", os.Getenv("GIT_CONFIG_NOSYSTEM"))
	require.Equal(t, "/dev/null", os.Getenv("GIT_CONFIG_GLOBAL"))
	require.Equal(t, "0", os.Getenv("GIT_TERMINAL_PROMPT"))
	require.Equal(t, "cat", os.Getenv("GIT_PAGER"))
	require.Equal(t, "cat", os.Getenv("PAGER"))
	require.Equal(t, "false", os.Getenv("GIT_EDITOR"))
	require.Equal(t, "false", os.Getenv("EDITOR"))
	require.Equal(t, "false", os.Getenv("VISUAL"))

	require.Equal(t, "C", os.Getenv("LC_ALL"))
	require.Equal(t, "C", os.Getenv("LANG"))
	require.Equal(t, "1", os.Getenv("NO_COLOR"))
	require.Equal(t, "dumb", os.Getenv("TERM"))

	for _, key := range []string{
		"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME",
	} {
		requireBelow(t, home, key)
	}

	for _, key := range []string{
		"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "all_proxy", "no_proxy",
		"GIT_ASKPASS", "SSH_AUTH_SOCK", "SSH_AGENT_PID", "SSH_ASKPASS",
	} {
		require.Empty(t, os.Getenv(key), "%s should be empty after Isolate", key)
	}
}

// TestIsolate_ScrubsAmbientCIEnv proves the host's CI/forge/token context
// cannot leak into a test (or a binary it spawns with os.Environ()): every
// such variable is cleared to empty regardless of what the developer's shell
// or the CI runner had set. reusable-ci's detection reads empty as absent.
func TestIsolate_ScrubsAmbientCIEnv(t *testing.T) {
	// Pollute the process env *before* Isolate, the way a CI runner would.
	polluted := map[string]string{
		"GITHUB_ACTIONS":        "true",
		"GITHUB_TOKEN":          "ghp_leaky",
		"GITHUB_OUTPUT":         "/host/output",
		"GITHUB_RUN_ID":         "999",
		"ACTIONS_RESULTS_URL":   "https://real.example",
		"ACTIONS_RUNTIME_TOKEN": "rt_leaky",
		"RUNNER_TEMP":           "/host/tmp",
		"CI":                    "true",
		"CI_JOB_TOKEN":          "gl_leaky",
		"GITLAB_CI":             "true",
		"FORGEJO_ACTIONS":       "true",
		"REUSABLE_CI_PROVIDER":  "gitlab",
		"ARTIFACT_NAME":         "hostleak",
		"REGISTRY_PASSWORD":     "s3cret",
		"DOCKER_CONFIG":         "/host/.docker",
		"GPG_PRIVATE_KEY":       "-----BEGIN-----",
		"GH_TOKEN":              "gh_leaky",
	}
	for key, value := range polluted {
		t.Setenv(key, value)
	}

	isolatedenv.Isolate(t)

	for key := range polluted {
		require.Empty(t, os.Getenv(key), "ambient %s must be scrubbed by Isolate", key)
	}
}

// TestIsolate_AHostTemplateHookDoesNotReachAScratchRepo asserts the effect
// rather than the variable, which is the difference that matters here.
//
// This file used to pin GIT_HOOKS_PATH=/dev/null. That assertion passed for
// as long as the line existed and proved nothing, because git has no such
// variable — the guard was inert and the test could not tell. GIT_TEMPLATE_DIR
// is the variable git does honour: `git init` copies its hooks into every new
// repo, whatever the config says. So the test now does what the old one only
// looked like it did — it plants a hook the way a host would and checks that
// none arrives.
func TestIsolate_AHostTemplateHookDoesNotReachAScratchRepo(t *testing.T) {
	// A hostile template directory, as an ambient host value: set before
	// Isolate, exactly as a developer's shell or a CI image would have it.
	hostile := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(hostile, "hooks"), 0o700))
	require.NoError(t, os.WriteFile( //nolint:gosec // 0700: the hook must be executable or it proves nothing.
		filepath.Join(hostile, "hooks", "pre-commit"),
		[]byte("#!/bin/sh\nexit 1\n"), 0o700))
	t.Setenv("GIT_TEMPLATE_DIR", hostile)

	isolatedenv.Isolate(t)

	repo := t.TempDir()
	cmd := exec.CommandContext(t.Context(), "git", "init", "-q", "-b", "main", repo) //nolint:gosec // fixed argv; repo is this test's own t.TempDir().
	cmd.Env = os.Environ()

	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git init: %s", out)

	entries, err := os.ReadDir(filepath.Join(repo, ".git", "hooks"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read hooks dir: %v", err)
	}

	require.Emptyf(t, entries,
		"Isolate left the host's GIT_TEMPLATE_DIR in place, so `git init` seeded %d hook(s) "+
			"into a scratch repo; every commit a test makes would run host-supplied code", len(entries))
}

// requireBelow requires the variable to name a path strictly inside root,
// judged by path components: a string prefix would also accept a sibling such
// as "<root>2/x".
func requireBelow(t *testing.T, root, key string) {
	t.Helper()

	require.True(t, below(root, os.Getenv(key)), "%s = %q, want a path inside %q", key, os.Getenv(key), root)
}

func below(root, path string) bool {
	rel, err := filepath.Rel(root, path)

	return err == nil && filepath.IsLocal(rel) && rel != "."
}

// TestBelow_JudgesByComponent keeps the containment helper honest: a sibling
// sharing the root as a string prefix, the root itself and a climbing path are
// outside.
func TestBelow_JudgesByComponent(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "home")

	for _, tc := range []struct {
		value  string
		inside bool
	}{
		{filepath.Join(root, ".gnupg"), true},
		{root + "2", false},
		{filepath.Join(root+"2", ".gnupg"), false},
		{root, false},
		{filepath.Join(root, "..", "other"), false},
	} {
		if got := below(root, tc.value); got != tc.inside {
			t.Errorf("below(%q, %q) = %t, want %t", root, tc.value, got, tc.inside)
		}
	}
}

// TestIsolate_ScrubsEveryFamilyAndRestoresAfterTheScope plants one canary for
// every scrubbed prefix and exact name, including the forge token chains, in
// the parent test. Inside a subtest that isolates, each is empty; once the
// subtest ends, each has its planted value again, so a nested isolation never
// leaks its scrub into the scope that called it.
func TestIsolate_ScrubsEveryFamilyAndRestoresAfterTheScope(t *testing.T) {
	canaries := map[string]string{}

	for _, prefix := range []string{"GITHUB_", "ACTIONS_", "RUNNER_", "GITLAB_", "CI_", "FORGEJO_", "GITEA_", "REUSABLE_CI_", "ARTIFACT_", "REGISTRY_", "NPM_", "SIGN_", "SIGSTORE_", "COSIGN_", "GPG_", "SSH_"} {
		canaries[prefix+"ISOLATION_CANARY"] = "planted-" + prefix
	}

	for _, key := range []string{
		"CI", "CONTINUOUS_INTEGRATION", "BUILD_NUMBER", "BUILDKITE", "CIRCLECI", "TRAVIS", "DRONE", "APPVEYOR",
		"JENKINS_URL", "TEAMCITY_VERSION", "GH_TOKEN", "GH_HOST", "GH_ENTERPRISE_TOKEN", "DOCKER_CONFIG", "DOCKER_AUTH_CONFIG",
	} {
		canaries[key] = "planted-" + key
	}

	for _, key := range append(runcontext.Token().Keys(), runcontext.ReleaseToken().Keys()...) {
		canaries[key] = "planted-" + key
	}

	for key, value := range canaries {
		t.Setenv(key, value)
	}

	t.Run("isolated", func(t *testing.T) {
		isolatedenv.Isolate(t)

		for key := range canaries {
			require.Empty(t, os.Getenv(key), "%s was not scrubbed", key)
		}
	})

	for key, value := range canaries {
		require.Equal(t, value, os.Getenv(key), "%s was not restored after the isolated scope", key)
	}
}
