// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Function
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package isolatedenv_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/testutil/isolatedenv"
)

func TestIsolate_ScrubsAndPinsProcessEnv(t *testing.T) {
	home := isolatedenv.Isolate(t)
	require.Equal(t, home, os.Getenv("HOME"))
	require.Equal(t, home, os.Getenv("USERPROFILE"))
	require.True(t, strings.HasPrefix(os.Getenv("TMPDIR"), home),
		"TMPDIR should be under home %q, got %q", home, os.Getenv("TMPDIR"))
	require.True(t, strings.HasPrefix(os.Getenv("GNUPGHOME"), home),
		"GNUPGHOME should be under isolated home %q, got %q", home, os.Getenv("GNUPGHOME"))

	require.Equal(t, "1", os.Getenv("GIT_CONFIG_NOSYSTEM"))
	require.Equal(t, "/dev/null", os.Getenv("GIT_CONFIG_GLOBAL"))
	require.Equal(t, "0", os.Getenv("GIT_TERMINAL_PROMPT"))
	require.Equal(t, "/dev/null", os.Getenv("GIT_HOOKS_PATH"))
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
		require.True(t, strings.HasPrefix(os.Getenv(key), home),
			"%s should be under home %q, got %q", key, home, os.Getenv(key))
	}

	for _, key := range []string{
		"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "all_proxy", "no_proxy",
		"GIT_ASKPASS", "GIT_CREDENTIAL_HELPER", "SSH_AUTH_SOCK", "SSH_AGENT_PID", "SSH_ASKPASS",
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
