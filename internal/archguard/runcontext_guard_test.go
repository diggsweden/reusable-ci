// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archguard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
)

// guardedLayers are the layers that must not name a run-context variable
// themselves. The rule they share:
//
//	Only an adapter may name a forge's variables, because an adapter IS a
//	dialect. Everyone else resolves the shared chain.
//
// adapters is therefore the ONE production exemption, principled rather than
// pragmatic. adapters/github/context.go reading $GITHUB_SHA, or
// adapters/gitlab reading $CI_SERVER_URL, is that adapter doing its job:
// normalizing one forge's names into a neutral Context. cienv's EventName
// doc turns on exactly this -- GitLab's CI_PIPELINE_SOURCE is kept OUT of
// the shared chain precisely so the gitlab adapter can normalize its
// dialect. A rule forbidding those reads would forbid the provider-adapter
// design itself.
//
// The failure this catches is narrow and real: a hand-written read resolves
// the names its author remembered, so it drifts from the chain beside it.
// Every instance found in this codebase had drifted -- app was blind to
// $CI_TEMP_DIR; cienv's OWN provenance helpers had lost $FORGEJO_SERVER and
// $CI_REPO from the SLSA builder identity; the release prerequisites check
// demanded $RELEASE_TOKEN while the push it gates accepted four more names.
//
// The fix differs by layer, which is why the message names both:
//
//   - domain/app are the INSIDE: they receive the run context. Add a flag
//     with Sources: cienv.X(), carry it on the use case's *Input.
//   - cli is the composition root: it may read the environment, but through
//     the shared chain -- a flag, or runcontext.X().Resolve(os.Getenv) when
//     a flag is wrong. Secrets are the case where a flag IS wrong: argv is
//     world-readable, so validate/prerequisites.go keeps its os.Getenv and
//     only borrows the chain.
//   - utilities accept values or injected lookups; moving a direct read to
//     a provider-neutral helper must not evade the same rule.
func guardedLayers() []string { return []string{"domain", "app", "cli", "utility"} }

// ownedNames returns every environment variable runcontext defines,
// derived from runcontext.All() rather than restated here -- a hand-copied
// list would be a second source of truth, which is the failure runcontext
// exists to end.
func ownedNames() map[string]string {
	owned := make(map[string]string)

	for _, v := range runcontext.All() {
		for _, key := range v.Keys() {
			owned[key] = v.Concept
		}
	}

	return owned
}

// TestLayersDoNotNameRunContextVars fails when a guarded layer resolves a
// run-context variable by name instead of through the shared chain.
//
// cienv itself is NOT exempt: it is the package the rule exists to protect,
// and it is where the rule was most visibly broken -- its provenance helpers
// hand-rolled chains that had drifted from the ones it publishes ten lines
// away. It resolves runcontext.Var against os.Getenv now, like any other
// caller that needs a value rather than a flag.
func TestLayersDoNotNameRunContextVars(t *testing.T) {
	t.Parallel()

	owned := ownedNames()
	packages, imp, fset := envGuardPackages(t, "./internal/...")
	checked := 0

	for _, pkg := range packages {
		rel, internal := strings.CutPrefix(pkg.ImportPath, internalPrefix)
		if !internal {
			continue
		}

		layer := layerOf(rel)
		if !slices.Contains(guardedLayers(), layer) {
			continue
		}

		files := make([]*ast.File, 0, len(pkg.CompiledGoFiles))
		for _, path := range pkg.CompiledGoFiles {
			if !filepath.IsAbs(path) {
				path = filepath.Join(pkg.Dir, path)
			}

			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}

			files = append(files, file)
		}

		checked += len(files)

		reads, err := runContextEnvReads(fset, pkg.ImportPath, files, imp)
		if err != nil {
			t.Fatalf("check %s: %v", pkg.ImportPath, err)
		}

		for _, read := range reads {
			t.Errorf("%s: reads $%s directly; %q run context is owned by internal/runcontext (%s).\n    Fix: %s",
				fset.Position(read.pos), read.name, owned[read.name], conceptNames(read.name, owned), runContextFix(layer))
		}
	}

	if checked == 0 {
		t.Fatal("env guard checked no production files")
	}
}

type envGuardPackage struct {
	Dir             string   `json:"Dir"`
	ImportPath      string   `json:"ImportPath"`
	Export          string   `json:"Export"`
	CompiledGoFiles []string `json:"CompiledGoFiles"`
}

// Use the installed compiler's export data, not guessed import stubs or partial
// type information. go list only builds metadata/export archives; it runs no
// application or provider code. This scans production files selected by go list's
// GOOS/GOARCH/GOFLAGS, not tests, testdata, or excluded platform/tag variants.
// Command-line -tags on an outer go test are not inherited; use GOFLAGS for tags.
func envGuardPackages(t *testing.T, patterns ...string) ([]envGuardPackage, types.Importer, *token.FileSet) {
	t.Helper()

	args := append([]string{"list", "-mod=readonly", "-buildvcs=false", "-deps", "-export", "-compiled", "-json"}, patterns...)
	goRoot := runtime.GOROOT()                                                           //nolint:staticcheck // SA1019: export data must use the build toolchain's Go, not a potentially different Go on PATH.
	cmd := exec.CommandContext(t.Context(), filepath.Join(goRoot, "bin", "go"), args...) //nolint:gosec // matching installed Go executable; callers supply fixed source/metadata package patterns.
	cmd.Dir = reporoot.Path(t)

	cmd.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local")

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	data, err := cmd.Output()
	if err != nil {
		t.Fatalf("env guard export metadata: %v\n%s", err, &stderr)
	}

	var packages []envGuardPackage

	exports := make(map[string]string)

	decoder := json.NewDecoder(bytes.NewReader(data))
	for decoder.More() {
		var pkg envGuardPackage
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatalf("env guard metadata: %v", err)
		}

		packages = append(packages, pkg)
		exports[pkg.ImportPath] = pkg.Export
	}

	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		archive := exports[path]
		if archive == "" {
			return nil, fmt.Errorf("no export archive for %s: %w", path, os.ErrNotExist)
		}

		return os.Open(archive)
	})

	return packages, imp, fset
}

type runContextEnvRead struct {
	pos  token.Pos
	name string
}

// runContextFix states the remedy in the terms the offending layer can act on.
// domain/app cannot import cienv at all (the layering forbids it), so
// telling them to "use cienv" would be advice they cannot take.
func runContextFix(layer string) string {
	if layer == "cli" {
		return "bind a flag with Sources: cienv.<Concept>(), or -- when the value is\n" +
			"    needed outside a flag (a helper, or a secret that must not reach argv) --\n" +
			"    resolve the shared chain: runcontext.<Concept>().Resolve(os.Getenv)."
	}

	if layer == "utility" {
		return "accept the value or an injected lookup; resolve runcontext.<Concept>()\n" +
			"    against that lookup rather than naming a forge variable."
	}

	return "this layer receives the run context, it does not look for it. Give the\n" +
		"    command a flag with Sources: cienv.<Concept>(), carry the value on the use\n" +
		"    case's *Input, and delete the read."
}

// runContextEnvReads recognizes stdlib object identity and Go string constants,
// including sibling-file/imported constants. Function values are deliberately
// bounded to single-write, non-address-taken variables within this package.
// Reassigned/escaped functions, imported function variables, wrappers, parameters,
// fields, containers and runtime-computed keys need review, not an invented flow
// result. Passing os.Getenv to runcontext.Resolve is not a direct named read.
//
// The bound is forced, not lazy, and it is worth knowing why before trying to
// tighten it. A rule that treats any `func(string) string` callee as an
// environment read was written and reverted: it flags the SANCTIONED seam, in
// which a use case accepts an injected lookup from the composition root, and
// the "injected lookup" and "shadowed package parameter" fixtures in
// runcontext_envsemantic_test.go pin that pattern as permitted. No local rule
// separates a lookup handed in by the composition root from one smuggled
// through a field, so failing closed here means failing on the approved design.
//
// So the supported claim is "this reports the flows it models", not "no
// unmodelled flow exists". Closing that gap needs interprocedural analysis that
// can tell an injected lookup from a smuggled one. docs/testing.md says the
// same thing for readers who never open this file.
func runContextEnvReads(fset *token.FileSet, path string, files []*ast.File, imp types.Importer) ([]runContextEnvRead, error) { //nolint:gocognit,gocyclo // bounded write accounting and object-based call recognition form one scanner, not a general flow engine.
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}

	config := types.Config{Importer: imp, Sizes: types.SizesFor("gc", runtime.GOARCH)}
	if _, err := config.Check(path, fset, files, info); err != nil {
		return nil, err
	}

	object := func(expr ast.Expr) types.Object {
		switch expr := ast.Unparen(expr).(type) {
		case *ast.Ident:
			return info.ObjectOf(expr)
		case *ast.SelectorExpr:
			return info.ObjectOf(expr.Sel)
		default:
			return nil
		}
	}
	values := make(map[types.Object]ast.Expr)
	writes := make(map[types.Object]int)
	escaped := make(map[types.Object]bool)

	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.ValueSpec:
				for i, name := range node.Names {
					if len(node.Values) > 0 {
						obj := info.ObjectOf(name)

						writes[obj]++
						if len(node.Names) == len(node.Values) {
							values[obj] = node.Values[i]
						}
					}
				}
			case *ast.AssignStmt:
				for i, lhs := range node.Lhs {
					obj := object(lhs)

					writes[obj]++
					if len(node.Lhs) == len(node.Rhs) {
						values[obj] = node.Rhs[i]
					}
				}
			case *ast.RangeStmt:
				if node.Key != nil {
					writes[object(node.Key)]++
				}

				if node.Value != nil {
					writes[object(node.Value)]++
				}
			case *ast.UnaryExpr:
				if node.Op == token.AND {
					escaped[object(node.X)] = true
				}
			case *ast.Field:
				// Parameters/results are injected values, not single-write aliases.
				for _, name := range node.Names {
					escaped[info.ObjectOf(name)] = true
				}
			}

			return true
		})
	}

	var isEnvFunc func(ast.Expr, map[types.Object]bool) bool

	isEnvFunc = func(expr ast.Expr, seen map[types.Object]bool) bool {
		obj := object(expr)
		if fn, ok := obj.(*types.Func); ok {
			return fn.Pkg() != nil && fn.Pkg().Path() == "os" && fn.Parent() == fn.Pkg().Scope() &&
				(fn.Name() == "Getenv" || fn.Name() == "LookupEnv")
		}

		v, ok := obj.(*types.Var)
		if !ok || v.IsField() || seen[obj] || writes[obj] != 1 || escaped[obj] || values[obj] == nil {
			return false
		}
		// A local call before the assignment is not evidence of its later value.
		if v.Parent() != v.Pkg().Scope() && expr.Pos() < values[obj].Pos() {
			return false
		}

		seen[obj] = true

		return isEnvFunc(values[obj], seen)
	}

	var reads []runContextEnvRead

	owned := ownedNames()

	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 || !isEnvFunc(call.Fun, make(map[types.Object]bool)) {
				return true
			}

			value := info.Types[call.Args[0]].Value
			if value != nil && value.Kind() == constant.String {
				name := constant.StringVal(value)
				if _, owns := owned[name]; owns {
					reads = append(reads, runContextEnvRead{pos: call.Pos(), name: name})
				}
			}

			return true
		})
	}

	return reads, nil
}

// conceptNames renders the full chain the offending name belongs to, so the
// failure shows what the single read is missing.
func conceptNames(name string, owned map[string]string) string {
	for _, v := range runcontext.All() {
		if owned[name] == v.Concept {
			return v.String()
		}
	}

	return "$" + name
}
