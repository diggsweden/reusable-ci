// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package isolatedenv scrubs the test process's environment so a test
// can't accidentally read the developer's ~/.gitconfig, trigger their
// SSH agent, pick up a stray $GNUPGHOME, inherit the CI runner's
// GITHUB_*/CI_*/REGISTRY_* context (which would steer forge detection,
// output sinks, and auth — and could leak a live token), or emit colour
// codes that break golden-file assertions.
//
// Use Isolate(t) at the top of any test that:
//
//   - shells out to git / gpg / ssh (they all read host env)
//   - reads XDG config dirs ($XDG_CONFIG_HOME, $XDG_DATA_HOME, …)
//   - detects the forge/runner or reads run-artifact/registry env
//   - spawns the built binary with os.Environ() (smoke harness)
//   - compares stdout/stderr byte-for-byte (NO_COLOR / TERM)
//
// Tests that already build an isolated git repo via
// testutil/isolatedgit.NewRepo get this scrub for free — NewRepo
// composes Isolate before configuring git-specific vars.
//
// WARNING: Isolate calls t.Setenv repeatedly, which is incompatible
// with t.Parallel() at the same scope. Subtests can still use Parallel
// — Go's test framework serialises Setenv at the outer test.
package isolatedenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scrubCIEnv clears every CI/forge/credential variable to empty (which
// reusable-ci's detection reads as "absent" — it uses os.Getenv()!="" /
// truthy, never presence). Cleared via t.Setenv so the host value is
// restored at test-end. A test that needs one of these set re-sets it
// explicitly *after* Isolate, and that later t.Setenv wins.
//
// Prefixes carry CI/forge context, runner wiring, provider/output overrides,
// run-artifact inputs, or registry credentials; the exact list is standalone
// CI/forge/credential vars with no shared prefix. A test must never inherit
// the developer's — or the CI runner's — real values for these: they steer
// forge detection, output-sink selection, and authentication, and some carry
// live tokens against real endpoints.
func scrubCIEnv(t *testing.T) {
	t.Helper()

	prefixes := []string{
		"GITHUB_", "ACTIONS_", "RUNNER_", // GitHub Actions runtime + artifact backend
		"GITLAB_", "CI_", // GitLab CI (CI_JOB_TOKEN, CI_API_V4_URL, …)
		"FORGEJO_", "GITEA_", // Forgejo / Gitea Actions
		"REUSABLE_CI_", "ARTIFACT_", // reusable-ci's own provider/runner/format + artifact verb inputs
		"REGISTRY_", // container/package registry auth
	}

	exact := map[string]struct{}{
		"CI": {}, "CONTINUOUS_INTEGRATION": {}, "BUILD_NUMBER": {},
		"BUILDKITE": {}, "CIRCLECI": {}, "TRAVIS": {}, "DRONE": {}, "APPVEYOR": {},
		"JENKINS_URL": {}, "TEAMCITY_VERSION": {},
		"GH_TOKEN": {}, "GH_HOST": {}, "GH_ENTERPRISE_TOKEN": {}, // gh CLI
		"DOCKER_CONFIG": {}, "DOCKER_AUTH_CONFIG": {}, // shared registry auth file
		"GPG_PRIVATE_KEY": {}, "GPG_PASSPHRASE": {}, // release gpg import
		"COSIGN_PASSWORD": {}, "COSIGN_PRIVATE_KEY": {}, "COSIGN_YES": {}, // signing
	}

	for _, kv := range os.Environ() {
		key, _, found := strings.Cut(kv, "=")
		if !found {
			continue
		}

		if _, ok := exact[key]; ok || hasAnyPrefix(key, prefixes) {
			t.Setenv(key, "")
		}
	}
}

func hasAnyPrefix(key string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}

	return false
}

// Isolate sets a curated set of environment variables pointing into
// throwaway directories under t.TempDir(). All values are restored at
// test-end by Go's t.Setenv cleanup.
//
// Returns the isolated $HOME path so callers that need to write into
// it (e.g. seed a fake gitconfig) can do so.
func Isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()

	// Core HOME isolation. USERPROFILE for Windows-targeting tests.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	// XDG base-directory spec. Tools that read XDG (e.g. mise, devbase,
	// most modern CLIs) look here first; pointing them at $HOME/.config
	// etc. means they can't reach the developer's real config.
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))

	// Git: disable system + global gitconfig reads. Tests that want a
	// gitconfig opt in by writing one and pointing GIT_CONFIG_GLOBAL at
	// it. Author/committer identity defaults provide stable signatures.
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_AUTHOR_NAME", "Test Bot")
	t.Setenv("GIT_AUTHOR_EMAIL", "bot@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Test Bot")
	t.Setenv("GIT_COMMITTER_EMAIL", "bot@example.invalid")

	// Interactive prompts / credential helpers / hooks — disable all.
	// A test should never hang waiting for password input or page in
	// less/more, and the editor must never fire.
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	t.Setenv("GIT_ASKPASS", "")
	t.Setenv("GIT_CREDENTIAL_HELPER", "")
	t.Setenv("GIT_HOOKS_PATH", "/dev/null")
	t.Setenv("GIT_SSH_COMMAND", "false")
	t.Setenv("GIT_PAGER", "cat")
	t.Setenv("GIT_EDITOR", "false")
	t.Setenv("EDITOR", "false")
	t.Setenv("VISUAL", "false")
	t.Setenv("PAGER", "cat")

	// GPG: own GNUPGHOME under the isolated tree so signing/verifying
	// tests don't touch the developer's keyring. The directory is
	// created on first use by gpg itself.
	t.Setenv("GNUPGHOME", filepath.Join(home, ".gnupg"))
	t.Setenv("GPG_TTY", "")

	// SSH agent isolation — empty values mean "no agent available", so
	// SSH operations fall back to keys-on-disk (and fail loudly if
	// they're missing) instead of silently using the developer's agent.
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("SSH_AGENT_PID", "")
	t.Setenv("SSH_ASKPASS", "")

	// Proxy isolation: prevent the developer's corp proxy / VPN config
	// from routing test traffic. Lowercase + uppercase forms are both
	// honoured by curl, go's net/http, and most CLIs.
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("NO_PROXY", "")
	t.Setenv("http_proxy", "")
	t.Setenv("https_proxy", "")
	t.Setenv("all_proxy", "")
	t.Setenv("no_proxy", "")

	// TMPDIR: pin to the isolated home so subprocess temp files don't
	// leak into the developer's /tmp (and races with parallel runs
	// can't collide on shared paths). Subprocesses (gpg, git) expect
	// the path to already exist, so create it eagerly.
	tmp := filepath.Join(home, "tmp")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		t.Fatalf("isolatedenv: mkdir TMPDIR %q: %v", tmp, err)
	}

	t.Setenv("TMPDIR", tmp)

	// Output stability: disable colour, force a terminal type that no
	// CLI tries to be clever about. Golden-file comparisons need bytes
	// that don't depend on the developer's TERM.
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	t.Setenv("CLICOLOR", "0")

	// Locale: force C locale so date / number formatting is stable
	// across the developer's LC_ALL settings.
	t.Setenv("LC_ALL", "C")
	t.Setenv("LANG", "C")

	// CI/forge/credential isolation: a test (and any binary it spawns with
	// os.Environ()) must not inherit ambient GITHUB_*/ACTIONS_*/CI_*/
	// REGISTRY_*/REUSABLE_CI_* etc. from the developer's shell or the CI
	// runner. Otherwise forge detection, sinks, and auth would depend on
	// where the suite happens to run, and real tokens could leak in.
	scrubCIEnv(t)

	return home
}
