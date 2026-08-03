// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli_test

import (
	"testing"

	ucli "github.com/urfave/cli/v3"

	rci "github.com/diggsweden/reusable-ci/v3/internal/cli"
)

// reservedExternalEnvVars are owned by external tools (git, gpg, docker,
// the OS) and carry a fixed meaning and type. reusable-ci must NOT reuse
// any of them as a flag value-source: e.g. git's $GIT_CONFIG_GLOBAL is a
// PATH, so wiring it to a bool flag breaks in every git-isolated
// environment (CI, this binary's own test harness), where the var is set
// to a config-file path that can't parse as a bool — the command then
// exits 70.
//
// The convention this guard enforces: reusable-ci's own behavioural
// controls use $REUSABLE_CI_*-prefixed env vars (REUSABLE_CI_LOG,
// REUSABLE_CI_GIT_CONFIG_GLOBAL, …). Standard *inputs* the tool
// legitimately consumes with matching semantics (GITHUB_*, CI_*,
// SOURCE_DATE_EPOCH, JAVA_VERSION, …) are fine and intentionally absent
// here.
//
//nolint:gochecknoglobals // test fixture: a read-only denylist.
var reservedExternalEnvVars = map[string]string{
	"GIT_CONFIG_GLOBAL":    "git: path to the global config file",
	"GIT_CONFIG_SYSTEM":    "git: path to the system config file",
	"GIT_DIR":              "git: repository .git directory",
	"GIT_WORK_TREE":        "git: working-tree root",
	"GIT_INDEX_FILE":       "git: index path",
	"GIT_OBJECT_DIRECTORY": "git: object store path",
	"GNUPGHOME":            "gpg: keyring home directory",
	"DOCKER_CONFIG":        "docker: config directory",
	"REGISTRY_AUTH_FILE":   "containers: auth-file path",
	"HOME":                 "OS: home directory",
	"PATH":                 "OS: executable search path",
	"XDG_CONFIG_HOME":      "XDG: config base directory",
	"XDG_CACHE_HOME":       "XDG: cache base directory",
}

// TestNoFlagSourcesCollideWithReservedEnvVars walks the whole command
// tree and fails if any flag sources a value from an env var owned by an
// external tool. See the reusable-ci-blackbox-tests finding that surfaced the
// original $GIT_CONFIG_GLOBAL → bool collision.
func TestNoFlagSourcesCollideWithReservedEnvVars(t *testing.T) {
	t.Parallel()

	root := rci.New(rci.BuildInfo{Version: "test"})

	var walk func(cmd *ucli.Command, path string)

	walk = func(cmd *ucli.Command, path string) {
		for _, f := range cmd.Flags {
			envFlag, ok := f.(interface{ GetEnvVars() []string })
			if !ok {
				continue
			}

			for _, env := range envFlag.GetEnvVars() {
				if why, reserved := reservedExternalEnvVars[env]; reserved {
					t.Errorf("`%s` flag %v sources reserved env $%s (%s) — "+
						"reusable-ci must not repurpose an external tool's variable; "+
						"use a $REUSABLE_CI_-prefixed name", path, f.Names(), env, why)
				}
			}
		}

		for _, sub := range cmd.Commands {
			walk(sub, path+" "+sub.Name)
		}
	}

	walk(root, root.Name)
}
