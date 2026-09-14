// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli_test

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	urfavecli "github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/cli"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/cliflags"
)

// TestRunContextEnvVarsGoThroughCienv walks every flag in the assembled
// command tree and fails when a run-context environment variable (the
// forge-provided and canonical bare names that internal/cli/cienv owns)
// is wired through an inline cli.EnvVars(...) chain instead of a cienv
// chain. Inline chains are how the same concept drifted into different
// names, orders, and fallbacks across commands; cienv is the single
// source of truth for these, so any inline use is a bug.
func TestRunContextEnvVarsGoThroughCienv(t *testing.T) {
	t.Parallel()

	offenders := runContextFlagViolations(t, cli.New(cli.BuildInfo{Version: "dev"}), runContextFlagExemptions())
	if len(offenders) > 0 {
		t.Errorf("run-context env vars wired outside cienv (use the matching cienv chain, "+
			"extending it if a name is missing):\n  %s", strings.Join(offenders, "\n  "))
	}
}

type runContextFlagKey struct{ command, flag, key string }

func runContextFlagExemptions() map[runContextFlagKey]string {
	// These release operations require an explicit final tag, never an ambient
	// branch/ref fallback. Verification likewise requires an explicit expected
	// tag. Each key is scoped separately so a new fallback cannot borrow this
	// exception; unused exceptions fail the same assembled-tree guard.
	return map[runContextFlagKey]string{
		{"reusable-ci version tag-release", "tag", "RELEASE_TAG"}:                        "explicit final tag to create",
		{"reusable-ci version tag-release", "tag", "TAG_NAME"}:                           "explicit final tag to create",
		{"reusable-ci version render-changelog", "tag", "RELEASE_TAG"}:                   "explicit final stable tag to render",
		{"reusable-ci version render-changelog", "tag", "TAG_NAME"}:                      "explicit final stable tag to render",
		{"reusable-ci version commit-changelog-release", "tag", "RELEASE_TAG"}:           "explicit final stable tag to commit and create",
		{"reusable-ci version commit-changelog-release", "tag", "TAG_NAME"}:              "explicit final stable tag to commit and create",
		{"reusable-ci container release-images validate", "expected-tag", "RELEASE_TAG"}: "explicit SLSA expected tag, not the current ref",
	}
}

func runContextFlagViolations(t *testing.T, root *urfavecli.Command, exemptions map[runContextFlagKey]string) []string {
	t.Helper()

	guarded := make(map[string]bool)

	for _, concept := range runcontext.All() {
		for _, key := range concept.Keys() {
			guarded[key] = true
		}
	}
	// Compare actual type identity, not a printable package-name substring.
	cienvType := reflect.TypeOf(cienv.Repository().Chain[0])
	used := make(map[runContextFlagKey]bool)

	var (
		offenders []string
		walk      func(path string, cmd *urfavecli.Command)
	)

	walk = func(path string, cmd *urfavecli.Command) {
		for _, flag := range cmd.Flags {
			for _, src := range cliflags.Sources(t, flag).Chain {
				env, ok := src.(interface{ Key() string })
				if !ok || !guarded[env.Key()] {
					continue
				}

				if reflect.TypeOf(src) == cienvType {
					continue
				}

				key := runContextFlagKey{path, flagName(flag), env.Key()}
				if reason := exemptions[key]; strings.TrimSpace(reason) != "" {
					used[key] = true

					continue
				}

				offenders = append(offenders,
					fmt.Sprintf("%s --%s reads $%s via an inline chain", path, key.flag, key.key))
			}
		}

		for _, sub := range cmd.Commands {
			walk(path+" "+sub.Name, sub)
		}
	}
	walk("reusable-ci", root)

	for key := range exemptions {
		if !used[key] {
			offenders = append(offenders, fmt.Sprintf("unused run-context exemption: %s --%s $%s", key.command, key.flag, key.key))
		}
	}

	slices.Sort(offenders)

	return offenders
}

func flagName(flag urfavecli.Flag) string {
	if names := flag.Names(); len(names) > 0 {
		return names[0]
	}

	return "?"
}
