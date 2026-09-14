// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cienv_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/urfave/cli/v3"
)

// Every accessor in cienv.go is one line with the same shape:
//
//	func Ref() cli.ValueSourceChain { return Sources(runcontext.Ref()) }
//
// Sixteen of them, differing only in the name inside the parentheses. That is
// exactly the shape a copy-paste gets wrong, and a wrong one is silent: --ref
// wired to runcontext.RefName() still resolves, still has env keys, still
// appears in --help. It reads the wrong variable, and the failure surfaces as a
// release that checked out something other than what the operator named.
//
// One accessor was spot-checked for a subset of its keys. These check all of
// them, against the owner each is supposed to bind, in order — and against the
// source, so the table cannot fall behind the file.

type accessorBinding struct {
	name     string
	accessor func() cli.ValueSourceChain
	owner    runcontext.Var
}

func accessorBindings() []accessorBinding {
	return []accessorBinding{
		{"Repository", cienv.Repository, runcontext.Repository()},
		{"RepositoryOwner", cienv.RepositoryOwner, runcontext.RepositoryOwner()},
		{"RefName", cienv.RefName, runcontext.RefName()},
		{"Ref", cienv.Ref, runcontext.Ref()},
		{"RefType", cienv.RefType, runcontext.RefType()},
		{"EventName", cienv.EventName, runcontext.EventName()},
		{"Commit", cienv.Commit, runcontext.Commit()},
		{"CheckoutRef", cienv.CheckoutRef, runcontext.CheckoutRef()},
		{"RunID", cienv.RunID, runcontext.RunID()},
		{"RunURL", cienv.RunURL, runcontext.RunURL()},
		{"Actor", cienv.Actor, runcontext.Actor()},
		{"ServerURL", cienv.ServerURL, runcontext.ServerURL()},
		{"TempDir", cienv.TempDir, runcontext.TempDir()},
		{"Workspace", cienv.Workspace, runcontext.Workspace()},
		{"Tag", cienv.Tag, runcontext.Tag()},
	}
}

func TestAccessors_BindTheirOwnersCompleteOrderedChain(t *testing.T) {
	for _, binding := range accessorBindings() {
		t.Run(binding.name, func(t *testing.T) {
			chain := binding.accessor()
			got := chain.EnvKeys()
			want := binding.owner.Keys()

			if len(want) == 0 {
				t.Fatalf("runcontext.%s declares no keys; the owner, not the accessor, is what was measured", binding.name)
			}

			if !slices.Equal(got, want) {
				t.Errorf("cienv.%s exposes %v, its owner declares %v.\n"+
					"Order is the precedence an operator relies on, and a missing key is a variable that stops "+
					"being read at all.", binding.name, got, want)
			}
		})
	}
}

// The value each accessor returns must come from its own keys and no others.
// A distinct sentinel per key makes "it resolved something" different from "it
// resolved the right thing", and the precedence assertion catches an accessor
// that reads its chain in the wrong order.
func TestAccessors_ResolveTheirOwnKeysInPrecedenceOrder(t *testing.T) {
	for _, binding := range accessorBindings() {
		t.Run(binding.name, func(t *testing.T) {
			keys := binding.owner.Keys()

			// Clear every key any accessor owns, so a value left by another
			// test or the developer's shell cannot answer for this one.
			for _, other := range accessorBindings() {
				for _, key := range other.owner.Keys() {
					t.Setenv(key, "")
				}
			}

			// Fill this accessor's chain with per-key sentinels, then take them
			// away one at a time: each step must yield the next key's value.
			for _, key := range keys {
				t.Setenv(key, "sentinel-for-"+key)
			}

			for i, key := range keys {
				chain := binding.accessor()

				value, found := chain.Lookup()
				if !found {
					t.Fatalf("with %v set, %s resolved nothing", keys[i:], binding.name)
				}

				if want := "sentinel-for-" + key; value != want {
					t.Fatalf("%s resolved %q, want %q; the chain is not read in declared order", binding.name, value, want)
				}

				t.Setenv(key, "")
			}

			empty := binding.accessor()
			if _, found := empty.Lookup(); found {
				t.Errorf("%s resolved a value with every one of its keys empty; it is reading something it does not own", binding.name)
			}
		})
	}
}

// The table above is hand-written, so it can fall behind the file. This reads
// the accessors out of the source and requires the two to agree, which is what
// stops a new accessor from being added with no binding check at all.
func TestAccessorBindings_CoverEveryAccessorInTheFile(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("cienv.go")
	if err != nil {
		t.Fatal(err)
	}

	file, err := parser.ParseFile(token.NewFileSet(), "cienv.go", source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}

	declared := map[string]string{}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || !fn.Name.IsExported() || fn.Name.Name == "Sources" {
			continue
		}

		if owner, found := singleOwnerCall(fn); found {
			declared[fn.Name.Name] = owner
		} else {
			t.Errorf("cienv.%s is an exported accessor this test cannot read; if it is not a plain "+
				"Sources(runcontext.X()) binding, give it its own coverage and exclude it here", fn.Name.Name)
		}
	}

	if len(declared) == 0 {
		t.Fatal("no accessors parsed out of cienv.go; the parser, not the file, is what was measured")
	}

	tabled := map[string]bool{}
	for _, binding := range accessorBindings() {
		tabled[binding.name] = true
	}

	for name, owner := range declared {
		if !tabled[name] {
			t.Errorf("cienv.%s binds runcontext.%s but is not in accessorBindings; nothing checks it reaches the right owner", name, owner)
		}

		if owner != name {
			t.Errorf("cienv.%s binds runcontext.%s. That may be deliberate, but the names differing is exactly "+
				"the copy-paste this file exists to catch, so it needs saying out loud here.", name, owner)
		}
	}

	for name := range tabled {
		if _, ok := declared[name]; !ok {
			t.Errorf("accessorBindings names %s, which cienv.go no longer declares", name)
		}
	}
}

// singleOwnerCall reads `return Sources(runcontext.X())` and returns X.
func singleOwnerCall(fn *ast.FuncDecl) (string, bool) {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return "", false
	}

	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return "", false
	}

	outer, ok := ret.Results[0].(*ast.CallExpr)
	if !ok || len(outer.Args) != 1 {
		return "", false
	}

	if name, isIdent := outer.Fun.(*ast.Ident); !isIdent || name.Name != "Sources" {
		return "", false
	}

	inner, ok := outer.Args[0].(*ast.CallExpr)
	if !ok {
		return "", false
	}

	selector, ok := inner.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}

	pkg, ok := selector.X.(*ast.Ident)
	if !ok || pkg.Name != "runcontext" {
		return "", false
	}

	return strings.TrimSpace(selector.Sel.Name), true
}
