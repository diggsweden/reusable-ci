// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	urfavecli "github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/cli"
)

// TestRunContextEnvVarsGoThroughCienv walks every flag in the assembled
// command tree and fails when a run-context environment variable (the
// forge-provided and canonical bare names that internal/cli/cienv owns)
// is wired through an inline cli.EnvVars(...) chain instead of a cienv
// chain. Inline chains are how the same concept drifted into different
// names, orders, and fallbacks across commands; cienv is the single
// source of truth for these, so any inline use is a bug.
//
// The tag family (RELEASE_TAG, TAG_NAME) is deliberately NOT guarded:
// several signing/verification verbs take a deliberate per-command
// RELEASE_TAG as an explicit expected-value contract, which is narrower
// than cienv.Tag()'s ref-name fallbacks and must stay that way.
func TestRunContextEnvVarsGoThroughCienv(t *testing.T) {
	t.Parallel()

	guarded := map[string]bool{
		// repository
		"REPOSITORY": true, "CI_REPO": true, "FORGEJO_REPOSITORY": true,
		"FORGEJO_REPO": true, "GITHUB_REPOSITORY": true,
		// refs
		"REF_NAME": true, "CI_REF_NAME": true, "FORGEJO_REF_NAME": true, "GITHUB_REF_NAME": true,
		"REF": true, "FORGEJO_REF": true, "GITHUB_REF": true,
		"REF_TYPE": true, "FORGEJO_REF_TYPE": true, "GITHUB_REF_TYPE": true,
		// commit
		"CI_COMMIT": true, "CI_COMMIT_SHA": true, "COMMIT_SHA": true,
		"FORGEJO_SHA": true, "GITHUB_SHA": true,
		// run identity
		"CI_RUN_ID": true, "FORGEJO_RUN_ID": true, "GITHUB_RUN_ID": true,
		"CI_RUN_URL": true, "CI_ACTOR": true,
		// server
		"CI_SERVER_URL": true, "FORGEJO_SERVER_URL": true,
		"FORGEJO_SERVER": true, "GITHUB_SERVER_URL": true,
		// dirs
		"CI_TEMP_DIR": true, "RUNNER_TEMP": true,
		"CI_WORKSPACE": true, "FORGEJO_WORKSPACE": true, "GITHUB_WORKSPACE": true,
		// tokens
		"CI_TOKEN": true, "FORGEJO_TOKEN": true, "GITHUB_TOKEN": true, "RELEASE_TOKEN": true,
	}

	root := cli.New(cli.BuildInfo{Version: "dev"})

	var offenders []string

	var walk func(path string, cmd *urfavecli.Command)

	walk = func(path string, cmd *urfavecli.Command) {
		for _, flag := range cmd.Flags {
			for _, src := range flagSources(flag).Chain {
				env, ok := src.(interface{ Key() string })
				if !ok || !guarded[env.Key()] {
					continue
				}

				if !strings.Contains(fmt.Sprintf("%T", src), "cienv.") {
					offenders = append(offenders,
						fmt.Sprintf("%s --%s reads $%s via an inline chain", path, flagName(flag), env.Key()))
				}
			}
		}

		for _, sub := range cmd.Commands {
			walk(path+" "+sub.Name, sub)
		}
	}
	walk("reusable-ci", root)

	if len(offenders) > 0 {
		t.Errorf("run-context env vars wired outside cienv (use the matching cienv chain, "+
			"extending it if a name is missing):\n  %s", strings.Join(offenders, "\n  "))
	}
}

// flagSources extracts the Sources chain from any urfave/cli flag type via
// reflection (all concrete flag types embed a FlagBase with a Sources field).
func flagSources(flag urfavecli.Flag) urfavecli.ValueSourceChain {
	val := reflect.ValueOf(flag)
	if val.Kind() == reflect.Pointer {
		val = val.Elem()
	}

	field := val.FieldByName("Sources")
	if !field.IsValid() {
		return urfavecli.ValueSourceChain{}
	}

	chain, ok := field.Interface().(urfavecli.ValueSourceChain)
	if !ok {
		return urfavecli.ValueSourceChain{}
	}

	return chain
}

func flagName(flag urfavecli.Flag) string {
	if names := flag.Names(); len(names) > 0 {
		return names[0]
	}

	return "?"
}
