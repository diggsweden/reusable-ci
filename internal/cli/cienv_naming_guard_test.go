// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	urfavecli "github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/cli"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/cliflags"
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
			concept, ok := conceptOf(t, flag)
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
func conceptOf(t *testing.T, flag urfavecli.Flag) (string, bool) {
	t.Helper()

	var keys []string

	for _, src := range cliflags.Sources(t, flag).Chain {
		if env, ok := src.(interface{ Key() string }); ok {
			keys = append(keys, env.Key())
		}
	}

	if len(keys) == 0 {
		return "", false
	}

	for _, v := range runcontext.All() {
		if slices.Equal(keys, v.Keys()) {
			return v.Concept, true
		}
	}

	return "", false
}

func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

// TestNoFlagCarriesAPartialRunContextChain is the drift the naming guard cannot
// see, because it only compares flags that matched a concept.
//
// conceptOf matches a flag by its env keys being EXACTLY a concept's list. A
// flag whose chain is a partial copy — the right variables in the right order
// but one missing, or one extra bolted on — matches nothing, is skipped, and so
// never reaches the one-name-per-concept rule either. It resolves differently
// from every other binding of the same concept, on exactly the runner where the
// missing variable is the one that is set, and nothing says so.
//
// The rule is the one runcontext exists to enforce: a flag that reads an owned
// variable reads the whole chain that owns it. A flag that reads none is not
// this guard's business.
func TestNoFlagCarriesAPartialRunContextChain(t *testing.T) {
	t.Parallel()

	seenSites := map[string]bool{}

	// env key -> the concept that owns it.
	owners := map[string]string{}

	for _, v := range runcontext.All() {
		for _, key := range v.Keys() {
			owners[key] = v.Concept
		}
	}

	if len(owners) == 0 {
		t.Fatal("runcontext.All() declared no keys; the accessor, not the flags, is what was measured")
	}

	declared := declaredPartialChains()
	checked := 0

	var walk func(path string, cmd *urfavecli.Command)

	walk = func(path string, cmd *urfavecli.Command) {
		for _, flag := range cmd.Flags {
			keys := envKeysOf(t, flag)
			if len(keys) == 0 {
				continue
			}

			checked++

			site := path + " --" + flagName(flag)
			if want, known := declared[site]; known {
				seenSites[site] = true

				if !slices.Equal(keys, want) {
					t.Errorf("%s is a declared partial chain recorded as %v but now reads %v. "+
						"Either it was fixed, in which case drop the row, or it drifted again, in which case "+
						"the row needs to say what it is now and why.", site, want, keys)
				}

				continue
			}

			if _, exact := conceptOf(t, flag); exact {
				continue
			}

			reportPartialChain(t, site, keys, owners)
		}

		for _, sub := range cmd.Commands {
			walk(path+" "+sub.Name, sub)
		}
	}

	walk("reusable-ci", cli.New(cli.BuildInfo{Version: "dev"}))

	if checked == 0 {
		t.Fatal("no env-backed flags found; the walk, not the command tree, is what was measured")
	}

	// A declared site that no longer diverges must be removed, so the list
	// cannot outlive the thing it describes.
	for site := range declared {
		if !seenSites[site] {
			t.Errorf("declaredPartialChains names %q, which the command tree no longer has or which no longer "+
				"carries a partial chain; drop the row", site)
		}
	}
}

// declaredPartialChains records the flags that list their env sources by hand
// instead of binding cienv.Tag(), with what is known about each.
//
// These are not exempted because they are fine. They are recorded because
// changing them changes how a required input on a RELEASE command resolves, and
// that is a decision to make deliberately rather than as a side effect of
// adding this check. The guard's job here is to stop the list growing.
//
// Three of the four also invert precedence: runcontext.Tag() reads TAG_NAME
// before RELEASE_TAG, and these read RELEASE_TAG first. On a runner with both
// set they resolve to a different tag than every other binding of the concept,
// which is a real difference and not obviously the intended one.
// Each row records the chain the site has TODAY, not merely that it diverges.
// Recording only the site would let a declared flag change its sources freely,
// which is the drift this guard is about.
func declaredPartialChains() map[string][]string {
	return map[string][]string{
		// Arguably a different concept: the tag expected inside a SLSA
		// provenance envelope, not the tag this run is producing. If that is
		// right it should say so in the flag; if not it should bind cienv.Tag().
		"reusable-ci container release-images validate --expected-tag": {"RELEASE_TAG"},

		// Truncated AND in the opposite precedence to runcontext.Tag(), which
		// reads TAG_NAME first. On a runner with both set these resolve to a
		// different tag than every other binding of the concept.
		"reusable-ci version commit-changelog-release --tag": {"RELEASE_TAG", "TAG_NAME"},
		"reusable-ci version render-changelog --tag":         {"RELEASE_TAG", "TAG_NAME"},
		// This one creates the release tag.
		"reusable-ci version tag-release --tag": {"RELEASE_TAG", "TAG_NAME"},
	}
}

func conceptKeys(concept string) []string {
	for _, v := range runcontext.All() {
		if v.Concept == concept {
			return v.Keys()
		}
	}

	return nil
}

// envKeysOf lists the environment keys a flag reads, in chain order.
func envKeysOf(t *testing.T, flag urfavecli.Flag) []string {
	t.Helper()

	var keys []string

	for _, src := range cliflags.Sources(t, flag).Chain {
		if env, ok := src.(interface{ Key() string }); ok {
			keys = append(keys, env.Key())
		}
	}

	return keys
}

// reportPartialChain names the first owned key in an unmatched chain, which is
// the one that makes the flag a drifted copy of a concept rather than an
// unrelated variable.
func reportPartialChain(t *testing.T, site string, keys []string, owners map[string]string) {
	t.Helper()

	for _, key := range keys {
		concept, owned := owners[key]
		if !owned {
			continue
		}

		t.Errorf("%s reads %s, which runcontext owns as the %q concept, but its chain is %v rather than %v.\n"+
			"    A partial chain resolves differently from every other binding of the same concept, on the "+
			"runner where the missing variable is the one that is set. Bind it with cienv.<Concept>() instead "+
			"of listing sources by hand.", site, key, concept, keys, conceptKeys(concept))

		return
	}
}
