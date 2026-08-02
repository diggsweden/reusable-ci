// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli_test

import (
	"fmt"
	"strings"
	"testing"

	urfavecli "github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/cli"
)

// TestPlanScopesMatchCommandPaths pins every plan-file source to reality:
// its scope string must equal the command path it is attached to, and its
// key must be a name of the very flag it feeds. Without this, renaming a
// command or flag silently orphans the plan wiring — `plan write` would
// validate against the NEW name while the source keeps reading the OLD
// one, and every plan value would quietly stop arriving.
func TestPlanScopesMatchCommandPaths(t *testing.T) {
	t.Parallel()

	root := cli.New(cli.BuildInfo{Version: "dev"})

	var offenders []string

	scoped := 0

	var walk func(path string, cmd *urfavecli.Command)

	walk = func(path string, cmd *urfavecli.Command) {
		for _, flag := range cmd.Flags {
			for _, src := range flagSources(flag).Chain {
				plan, ok := src.(interface {
					Scope() string
					PlanKey() string
				})
				if !ok {
					continue
				}

				scoped++

				if plan.Scope() != path {
					offenders = append(offenders, fmt.Sprintf(
						"%s --%s: plan scope %q != command path %q", path, flagName(flag), plan.Scope(), path))
				}

				if !containsName(flag.Names(), plan.PlanKey()) {
					offenders = append(offenders, fmt.Sprintf(
						"%s --%s: plan key %q is not a name of this flag", path, flagName(flag), plan.PlanKey()))
				}
			}
		}

		for _, sub := range cmd.Commands {
			walk(path+" "+sub.Name, sub)
		}
	}

	for _, sub := range root.Commands {
		walk(sub.Name, sub)
	}

	if scoped == 0 {
		t.Fatal("no plan sources found in the command tree; the reflection hook broke")
	}

	if len(offenders) > 0 {
		t.Errorf("plan wiring drifted from the command tree:\n  %s", strings.Join(offenders, "\n  "))
	}
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}

	return false
}
