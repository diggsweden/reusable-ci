// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package syncguard

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
)

// Three things have to line up for a generated file to stay honest: a generator
// under cmd/, a just recipe that runs it, and a sync guard that fails when the
// output drifts. There are three such files today and the mapping is 1:1:1.
//
// What is not written down anywhere is the requirement itself. A fourth
// generator added with a recipe and no guard produces a file that can drift
// silently forever, and nothing about that is visible in review — the new
// generator looks exactly like the three that are covered. The same goes for a
// guard whose refresh advice names a recipe that has since been renamed: the
// failure message sends the contributor to a command that does not exist, which
// is worse than no advice, because they will conclude the guard is broken.
//
// A registry object tying the four together would be more structure than three
// entries deserve. What is worth having is the rule, enforced: every generator
// is reachable, every guard's advice runs, and nothing is orphaned.
func TestEveryGeneratorHasARecipeAndAGuard(t *testing.T) {
	t.Parallel()

	root := reporoot.Path(t)
	justfile := string(reporoot.ReadFile(t, "justfile"))

	generators := generatorCommands(t, root)
	if len(generators) < 3 {
		t.Fatalf("found %d generators under cmd/; the walk, not the tree, is what was measured", len(generators))
	}

	recipes := justRecipes(justfile)
	advice := guardRefreshCommands(t, root)

	for _, generator := range generators {
		recipe := strings.TrimPrefix(generator, "gen-")

		body, ok := recipes["gen-"+recipe]
		if !ok {
			t.Errorf("cmd/%s has no `just gen-%s` recipe; a generator nobody can run is a file that drifts", generator, recipe)

			continue
		}

		if !strings.Contains(body, "./cmd/"+generator) {
			t.Errorf("`just gen-%s` does not run ./cmd/%s; the recipe and the generator drifted apart", recipe, generator)
		}

		if !advice["just gen-"+recipe] {
			t.Errorf("no sync guard tells a contributor to run `just gen-%s`; cmd/%s produces a file nothing compares",
				recipe, generator)
		}
	}

	// The other direction: advice that names a recipe which no longer exists
	// sends a contributor to a command that fails.
	for command := range advice {
		recipe := strings.TrimPrefix(command, "just ")
		if _, ok := recipes[recipe]; !ok {
			t.Errorf("a sync guard advises %q, but the justfile has no such recipe; the failure message is a dead end", command)
		}
	}
}

// generatorCommands lists the cmd/gen-* directories, which is what a generator
// is in this repository.
func generatorCommands(t *testing.T, root string) []string {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(root, "cmd"))
	if err != nil {
		t.Fatal(err)
	}

	var found []string

	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "gen-") {
			found = append(found, entry.Name())
		}
	}

	sort.Strings(found)

	return found
}

// justRecipeHeader matches a recipe name at the start of a line, which is how
// just declares one.
var justRecipeHeader = regexp.MustCompile(`(?m)^([a-z][a-z0-9-]*)(?: [^:\n]*)?:\s*$`)

// justRecipes maps each recipe name to its indented body.
func justRecipes(justfile string) map[string]string {
	found := map[string]string{}
	lines := strings.Split(justfile, "\n")

	for i, line := range lines {
		match := justRecipeHeader.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		var body strings.Builder

		for _, next := range lines[i+1:] {
			if next != "" && !strings.HasPrefix(next, " ") && !strings.HasPrefix(next, "\t") {
				break
			}

			body.WriteString(next)
			body.WriteString("\n")
		}

		found[match[1]] = body.String()
	}

	return found
}

// refreshAdvice matches the second argument of generatedDifference, which is
// the command a guard tells a contributor to run.
var refreshAdvice = regexp.MustCompile(`generatedDifference\([^,]+,\s*"([^"]+)"`)

// guardRefreshCommands collects every refresh command the sync guards advise.
func guardRefreshCommands(t *testing.T, root string) map[string]bool {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(root, "internal", "syncguard"))
	if err != nil {
		t.Fatal(err)
	}

	found := map[string]bool{}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		// contract_boundary_test.go exercises generatedDifference itself with
		// invented arguments ("just refresh", "docs/example"). Those are
		// fixtures for the helper, not advice any guard gives.
		if entry.Name() == "contract_boundary_test.go" {
			continue
		}

		body := string(reporoot.ReadFile(t, filepath.Join("internal", "syncguard", entry.Name())))
		for _, match := range refreshAdvice.FindAllStringSubmatch(body, -1) {
			if strings.HasPrefix(match[1], "just ") {
				found[match[1]] = true
			}
		}
	}

	if len(found) == 0 {
		t.Fatal("no refresh advice found in the sync guards; the regexp, not the guards, is what was measured")
	}

	return found
}

// TestGeneratedOutputsAreCheckedByTheOfflineSuite closes the other half of the
// question: the guards must run where CI actually runs them.
//
// `just test` is what the pull-request workflow invokes, and these guards are
// ordinary Go tests under internal/syncguard, so they run there by
// construction. What is worth asserting is the part that is NOT structural —
// that the convenience `check-*` wrappers, which exist so a contributor can run
// one guard without the whole suite, still name tests that exist.
func TestGeneratedOutputsAreCheckedByTheOfflineSuite(t *testing.T) {
	t.Parallel()

	justfile := string(reporoot.ReadFile(t, "justfile"))

	// The wrappers point at tests across the guard packages, not only this
	// one — `just check-runtime-tags` runs a workflowguard test — so the search
	// covers all of them rather than a hand-listed few.
	defined := guardTestNames(t, reporoot.Path(t))

	named := regexp.MustCompile(`-run '\^(Test[A-Za-z0-9_]+)\$'`).FindAllStringSubmatch(justfile, -1)
	if len(named) == 0 {
		t.Fatal("no check-* wrapper names a test; the regexp, not the justfile, is what was measured")
	}

	for _, match := range named {
		if !defined[match[1]] {
			t.Errorf("a just recipe runs %q, which no guard package defines; the wrapper passes by selecting nothing",
				match[1])
		}
	}

	if !strings.Contains(string(reporoot.ReadFile(t, filepath.Join(".github", "workflows", "self-pullrequest.yml"))), "just test") {
		t.Error("the pull-request workflow no longer runs `just test`, which is how these guards reach CI at all")
	}
}

// guardTestNames collects every top-level test function defined in the
// repo-wide guard packages, which is the set a check-* wrapper may name.
func guardTestNames(t *testing.T, root string) map[string]bool {
	t.Helper()

	defined := map[string]bool{}
	definition := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)

	for _, pkg := range []string{"archguard", "lexiconguard", "syncguard", "workflowguard"} {
		entries, err := os.ReadDir(filepath.Join(root, "internal", pkg))
		if err != nil {
			t.Fatal(err)
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}

			body := string(reporoot.ReadFile(t, filepath.Join("internal", pkg, entry.Name())))
			for _, match := range definition.FindAllStringSubmatch(body, -1) {
				defined[match[1]] = true
			}
		}
	}

	if len(defined) == 0 {
		t.Fatal("no guard tests found; the walk, not the packages, is what was measured")
	}

	return defined
}
