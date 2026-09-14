// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli_test

import (
	"testing"

	ucli "github.com/urfave/cli/v3"

	rci "github.com/diggsweden/reusable-ci/v3/internal/cli"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/cliflags"
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

	inspected := 0
	seen := map[string]bool{}

	var walk func(cmd *ucli.Command, path string)

	walk = func(cmd *ucli.Command, path string) {
		for _, f := range cmd.Flags { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
			// cliflags.Sources rather than a GetEnvVars type assertion. The
			// assertion skipped any flag whose type did not satisfy it, so a
			// flag the guard could not read was indistinguishable from one
			// sourcing no environment at all, and the skip was silent -- which
			// is how a denylist reports compliance for a flag it never looked
			// at. Sources fails the test instead, and it is what the other
			// four CLI surface guards already use. Every flag type in the tree
			// satisfies GetEnvVars today, so this closes a latent hole rather
			// than a live one: both traversals read the same 1452 values.
			for _, src := range cliflags.Sources(t, f).Chain {
				env, ok := src.(interface{ Key() string })
				if !ok {
					continue
				}

				inspected++
				seen[env.Key()] = true

				if why, reserved := reservedExternalEnvVars[env.Key()]; reserved {
					t.Errorf("`%s` flag %v sources reserved env $%s (%s) — "+
						"reusable-ci must not repurpose an external tool's variable; "+
						"use a $REUSABLE_CI_-prefixed name", path, f.Names(), env.Key(), why)
				}
			}
		}

		for _, sub := range cmd.Commands {
			walk(sub, path+" "+sub.Name)
		}
	}

	walk(root, root.Name)

	// A denylist that inspects nothing is not a rule, and a bare nonzero count
	// is a weak way to say so: one readable root flag satisfies it while the
	// rest of the tree goes unvisited. The anchors are named variables the
	// traversal must reach, one on the root command and one that exists only
	// on leaves three levels down, so a walk that stopped descending fails
	// here instead of passing with a smaller number.
	for _, anchor := range []string{"REUSABLE_CI_LOG", "REUSABLE_CI_REGISTRY_AUTH_FILE"} {
		if !seen[anchor] {
			t.Errorf("the traversal never reached $%s; it is not auditing the surface it claims to", anchor)
		}
	}

	if inspected == 0 {
		t.Fatal("no env-sourced flags were inspected; the guard is checking nothing")
	}
}
