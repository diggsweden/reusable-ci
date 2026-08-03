// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli_test

import (
	"sort"
	"strings"
	"testing"

	urfavecli "github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/cli"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// TestOneFlagNamePerRunContextConcept walks the assembled command tree and
// fails when two flags bound to the SAME run-context concept are spelled
// differently.
//
// runcontext made every binding of a concept resolve the same value. This is
// the other half: a caller should not have to remember that the value is
// --temp-dir here and --runner-temp there. Both drifts this caught were real
// and invisible until measured — TempDir was split --runner-temp/--temp-dir
// across five commands, and Commit was --commit in three places and
// --commit-sha in a fourth.
//
// The rule is one PRIMARY name per concept; aliases are how a rename stays
// non-breaking, so they are not counted. Rename the odd one out and keep its
// former spelling in Aliases (and, when the flag reads a plan file, honour
// the old key too — the plan maps flag names, so a rename moves its key).
//
// A flag is matched to a concept by its env-source keys being exactly that
// concept's name list. Exact matching matters: Tag's chain is a superset of
// RefName's, so a subset test would call every --tag a --ref-name.
func TestOneFlagNamePerRunContextConcept(t *testing.T) {
	t.Parallel()

	byConcept := map[string]map[string][]string{} // concept -> primary name -> paths

	var walk func(path string, cmd *urfavecli.Command)

	walk = func(path string, cmd *urfavecli.Command) {
		for _, flag := range cmd.Flags {
			concept, ok := conceptOf(flag)
			if !ok {
				continue
			}

			if byConcept[concept] == nil {
				byConcept[concept] = map[string][]string{}
			}

			name := flagName(flag)
			byConcept[concept][name] = append(byConcept[concept][name], path)
		}

		for _, sub := range cmd.Commands {
			walk(path+" "+sub.Name, sub)
		}
	}

	walk("reusable-ci", cli.New(cli.BuildInfo{Version: "dev"}))

	for _, concept := range sortedKeys(byConcept) {
		spellings := byConcept[concept]
		if len(spellings) < 2 {
			continue
		}

		var b strings.Builder

		for _, name := range sortedKeys(spellings) {
			b.WriteString("\n      --" + name + ": " + strings.Join(spellings[name], ", "))
		}

		t.Errorf(
			"the %q run context is exposed under %d different flag names:%s\n"+
				"    One concept, one flag name. Pick the spelling most commands already\n"+
				"    use, rename the others to it, and keep each former spelling in\n"+
				"    Aliases so no caller breaks.",
			concept, len(spellings), b.String(),
		)
	}
}

// conceptOf reports which run-context concept a flag binds, matching on its
// env-source keys. Sources that carry no env key (a plan-file source) are
// ignored, so a plan-backed flag still matches the concept it falls back to.
func conceptOf(flag urfavecli.Flag) (string, bool) {
	var keys []string

	for _, src := range flagSources(flag).Chain {
		if env, ok := src.(interface{ Key() string }); ok {
			keys = append(keys, env.Key())
		}
	}

	if len(keys) == 0 {
		return "", false
	}

	for _, v := range runcontext.All() {
		if slicesEqual(keys, v.Names) {
			return v.Concept, true
		}
	}

	return "", false
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	sort.Strings(out)

	return out
}
