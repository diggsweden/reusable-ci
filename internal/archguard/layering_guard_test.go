// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package archguard enforces where code may live. It holds the executable
// form of ADR 0004 -- walking the import graph under internal/ and failing on
// any outward edge across a layer boundary -- alongside the other placement
// rules: which layer may read the environment, which may mint a credential,
// which may branch on the platform.
//
// One of the repo-wide guard packages; docs/testing.md says which is which and
// where a new guard belongs.
package archguard

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
)

const internalPrefix = "github.com/diggsweden/reusable-ci/v3/internal/"

// layerOf classifies an internal package path (given relative to internal/)
// into its hexagonal layer. Utilities may depend inward on domain but never
// on app, cli or adapters. Test infrastructure has an explicit separate layer.
// listval in particular is imported by domain itself, which is exactly why
// it cannot live inside one of the layered trees.
func layerOf(rel string) string {
	for _, layer := range []string{"domain", "adapters", "app", "cli"} {
		if rel == layer || strings.HasPrefix(rel, layer+"/") {
			return layer
		}
	}

	for _, prefix := range []string{"testutil", "livetest"} {
		if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
			return "test"
		}
	}

	return "utility"
}

// forbiddenEdges lists, per layer, the layers it may not import. Every arrow
// in the hexagon points inward at domain, so the rule is stated as the
// outward edges that must not exist:
//
//	cli  ──►  app  ──►  domain  ◄──  adapters
//
// domain is the strictest: it imports nothing but itself and leaf utilities,
// which is what lets it be tested without a runner, a network, or a binary.
func forbiddenEdges() map[string][]string {
	return map[string][]string{
		"domain":   {"app", "adapters", "cli"},
		"adapters": {"app", "cli"},
		"app":      {"adapters", "cli"},
		"utility":  {"app", "adapters", "cli"},
	}
}

// fixFor explains each violation in the terms a contributor needs to fix it,
// rather than restating the rule.
func fixFor(edge string) string {
	return map[string]string{
		"domain->app":       "domain holds rules, not sequencing; move the orchestration into internal/app and leave the rule behind.",
		"domain->adapters":  "domain must not know a concrete tool. Declare a small interface (a port) in domain and let the adapter satisfy it.",
		"domain->cli":       "domain must not know it is driven by a CLI. Pass the value in as a parameter instead.",
		"adapters->app":     "an adapter is driven by a use case, never the reverse. Invert the call, or move the shared logic into domain.",
		"adapters->cli":     "an adapter must not read flags. Take the value as a struct field or parameter and let cmd/ wire it.",
		"app->adapters":     "a use case must depend on a port, not a concrete tool. Declare the small interface it needs (see gitOps in app/validate/tags.go) and construct the adapter in internal/cli.",
		"app->cli":          "a use case must not parse flags or own stdout. Accept an io.Writer and plain inputs; wire it in internal/cli.",
		"utility->app":      "a shared utility must not orchestrate use cases; move orchestration into app or cli.",
		"utility->cli":      "a shared utility receives values, not CLI flags or composition code.",
		"utility->adapters": "a shared utility must not construct concrete adapters; accept a narrow interface or move composition into cli.",
	}[edge]
}

// outwardEdge is one forbidden import found in one file.
type outwardEdge struct {
	name     string // e.g. "app->adapters"
	imported string // the full import path
}

// violationsIn returns the forbidden edges the file at path introduces,
// given the layer it belongs to.
func violationsIn(fset *token.FileSet, path, from string, forbidden map[string][]string) ([]outwardEdge, error) {
	file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}

	var found []outwardEdge

	for _, spec := range file.Imports {
		imported, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, err
		}

		target, ok := strings.CutPrefix(imported, internalPrefix)
		if !ok {
			continue
		}

		to := layerOf(target)
		if to == "" || !slices.Contains(forbidden[from], to) {
			continue
		}

		found = append(found, outwardEdge{name: from + "->" + to, imported: imported})
	}

	return found, nil
}

// TestNoOutwardImports fails when any non-test file under internal/ imports
// a layer further out than its own. Test files are exempt on purpose: test
// code is a composition root, so app-layer tests such as
// app/validate/tags_test.go legitimately wire the real git adapter to
// exercise real behaviour.
func TestNoOutwardImports(t *testing.T) {
	t.Parallel()

	root := reporoot.Path(t)

	var (
		internalDir = filepath.Join(root, "internal")
		fset        = token.NewFileSet()
		forbidden   = forbiddenEdges()
		offenders   []string
	)

	walkErr := filepath.WalkDir(internalDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !isProductGoFile(entry, path) {
			return nil
		}

		rel, err := filepath.Rel(internalDir, filepath.Dir(path))
		if err != nil {
			return err
		}

		from := layerOf(filepath.ToSlash(rel))
		if from == "" {
			return nil
		}

		found, err := violationsIn(fset, path, from, forbidden)
		if err != nil {
			return err
		}

		relFile, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		for _, edge := range found {
			offenders = append(offenders,
				relFile+"\n    imports "+edge.imported+"\n    "+edge.name+" is forbidden: "+fixFor(edge.name))
		}

		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk internal/: %v", walkErr)
	}

	if len(offenders) > 0 {
		slices.Sort(offenders)
		t.Fatalf("outward imports break the layering (ADR 0004):\n\n%s\n\n"+
			"Every arrow must point inward at domain. If a use case needs a concrete\n"+
			"tool, declare the interface it needs at the point of use and construct\n"+
			"the adapter in internal/cli or cmd/ (see internal/app/validate/tags.go).",
			strings.Join(offenders, "\n\n"))
	}
}
