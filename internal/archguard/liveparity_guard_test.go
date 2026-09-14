// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archguard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
)

// This is a source-inventory guard, not proof of execution, assertions, or parity
// on every forge. Only command-position argv reaching imported CLI/CLIIn calls
// in Test bodies and their syntactically called helpers earns invocation credit.
// Shell probes, arbitrary function-valued fields, and external helpers do not.
func TestForgeAwareCommandsHaveALiveScenario(t *testing.T) {
	t.Parallel()
	root := reporoot.Path(t)
	fset := token.NewFileSet()

	paths, err := filepath.Glob(filepath.Join(root, "internal/livetest/conformance/*_test.go"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no scenario sources: %v", err)
	}

	files := make([]*ast.File, 0, len(paths))
	for _, path := range paths {
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}

		files = append(files, file)
	}

	covered, unresolved := liveInvocations(fset, files)
	for _, location := range unresolved {
		t.Logf("UNRESOLVED argv (no invocation credit): %s", location)
	}

	commandsDir := filepath.Join(root, "internal/cli/commands")

	entries, err := os.ReadDir(commandsDir)
	if err != nil {
		t.Fatal(err)
	}

	checked := 0
	// D179 debt dispositions, not coverage or forge-agnostic exemptions. Exact
	// groups only; new gaps fail, and an obsolete disposition must be removed.
	// No live execution or signer/provider activity is authorized by this test.
	dispositions := map[string]string{
		"build":     "DEFERRED: domain/build -> domain/summary -> provider is package-level awareness; native build verbs still need a verb-level parity decision",
		"config":    "DEFERRED: config validation reaches domain/build and provider-backed summary types; no config CLI conformance invocation",
		"lint":      "DEFERRED: the Git adapter reaches runcontext; no lint CLI conformance invocation, and native linter parity is not established",
		"plan":      "DEFERRED: pipeline planning reaches provider-backed domain types; no plan CLI conformance invocation",
		"publish":   "OUTSIDE SCANNER: forgepackages_test.go drives in-runner shell probes, not CLI/CLIIn; do not credit shell text or infer coverage of other publish verbs",
		"report":    "MISSING: report/status imports provider, but adapter capability checks and helper labels are not a report CLI scenario",
		"sbom":      "DEFERRED: cosign -> domain/container -> provider is package-level awareness; no sbom CLI conformance invocation",
		"toolchain": "DEFERRED: cienv reaches runcontext; no toolchain CLI conformance invocation, and native installer parity is not established",
		"version":   "MISSING (D179): commit-changelog-release reads runcontext and performs signed Git mutations; JSON version fields are not coverage; a separately authorized live scenario remains unimplemented",
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		chain, err := liveForgeDependency(os.DirFS(root), "internal/cli/commands/"+entry.Name())
		if err != nil {
			t.Fatal(err)
		}

		if len(chain) == 0 {
			continue
		}

		checked++

		if len(covered[entry.Name()]) == 0 {
			if reason, ok := dispositions[entry.Name()]; ok {
				t.Logf("UNCOVERED %s: %s\n    %s", entry.Name(), reason, strings.Join(chain, "\n    "))
				delete(dispositions, entry.Name())
			} else {
				t.Errorf("forge-aware command %q has no recognized live CLI invocation:\n    %s", entry.Name(), strings.Join(chain, "\n    "))
			}
		} else {
			t.Logf("INVOKED %s: %s", entry.Name(), strings.Join(covered[entry.Name()], ", "))
		}
	}

	if checked == 0 {
		t.Fatal("no forge-aware command groups found; check the command tree and markers")
	}

	for group := range dispositions {
		t.Errorf("stale live-parity disposition for %q: the group disappeared, is no longer forge-aware, or now has invocation coverage", group)
	}
}

// liveForgeDependency follows internal production imports, including tagged
// files, conservatively at package granularity. Shared deps can make a group
// aware even if an individual verb never uses the provider. It does not trace
// dynamic dispatch or claim that every imported marker affects every command.
func liveForgeDependency(tree fs.FS, dir string) ([]string, error) {
	return liveForgeDependencySeen(tree, dir, map[string]bool{})
}

func liveForgeDependencySeen(tree fs.FS, dir string, seen map[string]bool) ([]string, error) {
	if seen[dir] {
		return nil, nil
	}

	seen[dir] = true

	entries, err := fs.ReadDir(tree, dir)
	if err != nil {
		return nil, err
	}

	type edge struct{ target, location string }

	var edges []edge

	for _, entry := range entries {
		if !isProductGoFile(entry, entry.Name()) {
			continue
		}

		path := dir + "/" + entry.Name()

		source, err := fs.ReadFile(tree, path)
		if err != nil {
			return nil, err
		}

		fset := token.NewFileSet()

		file, err := parser.ParseFile(fset, path, source, parser.ImportsOnly)
		if err != nil {
			return nil, err
		}

		for _, spec := range file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return nil, err
			}

			location := fmt.Sprintf("%s imports %s", fset.Position(spec.Pos()), imported)
			if imported == internalPrefix+"domain/provider" || imported == internalPrefix+"runcontext" {
				return []string{location}, nil
			}

			if target, ok := strings.CutPrefix(imported, internalPrefix); ok {
				edges = append(edges, edge{"internal/" + target, location})
			}
		}
	}

	for _, edge := range edges {
		chain, err := liveForgeDependencySeen(tree, edge.target, seen)
		if err != nil {
			return nil, err
		}

		if len(chain) != 0 {
			return append([]string{edge.location}, chain...), nil
		}
	}

	return nil, nil
}

// liveHead retains only argv[0]; empty is a proven empty slice, whereas the zero
// value is unknown. Later flags, payloads, and helper labels cannot earn credit.
type liveHead struct {
	word  string
	empty bool
}

// liveLexicalObject is parser declaration identity, not type or selector
// resolution. This scanner uses only lexical bindings for the documented
// syntax subset; it deliberately does not type-check or execute live packages.
type liveLexicalObject = ast.Object //nolint:staticcheck // SA1019: parser bindings are intentional here; no type semantics are inferred.

// liveInvocations recognizes literals/constants, local argv, prefix-preserving
// append, single-return argv helpers, and direct variadic forwarding (including
// named receiver methods). Conditional replacement with a different head and
// function-valued fields are unresolved. This is not a Go interpreter: alias
// mutation, captured-state changes at a later call, and general interprocedural
// data flow are outside its model and need separate review.
//
//nolint:gocognit,gocyclo,maintidx // Keep the bounded syntax cases and their shared lexical state together, rather than introducing an analysis framework.
func liveInvocations(fset *token.FileSet, files []*ast.File) (map[string][]string, []string) {
	type function struct {
		params *ast.FieldList
		body   *ast.BlockStmt
	}

	type write struct {
		pos   token.Pos
		expr  ast.Expr
		block *ast.BlockStmt
	}

	functions := map[string]*ast.FuncDecl{}
	writes := map[*liveLexicalObject][]write{}
	recordWrite := func(lhs ast.Expr, pos token.Pos, expr ast.Expr, block *ast.BlockStmt) {
		lhs = ast.Unparen(lhs)
		if index, ok := lhs.(*ast.IndexExpr); ok {
			lhs, expr = ast.Unparen(index.X), nil
		}

		if id, ok := lhs.(*ast.Ident); ok && id.Obj != nil {
			writes[id.Obj] = append(writes[id.Obj], write{pos, expr, block})
		}
	}
	parents := map[ast.Node]ast.Node{}

	imports := map[*ast.File]map[string]string{}
	for _, file := range files {
		imports[file] = map[string]string{}
		for _, spec := range file.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)

			name := filepath.Base(path)
			if spec.Name != nil {
				name = spec.Name.Name
			}

			imports[file][name] = path
		}

		var stack []ast.Node

		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				stack = stack[:len(stack)-1]

				return false
			}

			if len(stack) > 0 {
				parents[node] = stack[len(stack)-1]
			}

			stack = append(stack, node)

			var block *ast.BlockStmt

			for _, parent := range stack {
				if b, ok := parent.(*ast.BlockStmt); ok {
					block = b
				}
			}

			switch n := node.(type) {
			case *ast.FuncDecl:
				key := n.Name.Name
				if n.Recv != nil {
					key = fmt.Sprint(n.Recv.List[0].Type) + "." + key
				}

				functions[key] = n
			case *ast.AssignStmt:
				for i, lhs := range n.Lhs {
					var value ast.Expr
					if (n.Tok == token.ASSIGN || n.Tok == token.DEFINE) && len(n.Rhs) == len(n.Lhs) {
						value = n.Rhs[i]
					}

					recordWrite(lhs, n.Pos(), value, block)
				}
			case *ast.RangeStmt:
				recordWrite(n.Key, n.Pos(), nil, block)
				recordWrite(n.Value, n.Pos(), nil, block)
			case *ast.IncDecStmt:
				recordWrite(n.X, n.Pos(), nil, block)
			case *ast.ValueSpec:
				for i, id := range n.Names {
					var value ast.Expr
					if len(n.Values) == len(n.Names) {
						value = n.Values[i]
					}

					recordWrite(id, n.Pos(), value, block)
				}
			}

			return true
		})
	}

	fileOf := func(node ast.Node) *ast.File {
		for node != nil {
			if file, ok := node.(*ast.File); ok {
				return file
			}

			node = parents[node]
		}

		return nil
	}
	testingParam := func(typ ast.Expr) bool {
		pointer, ok := typ.(*ast.StarExpr)
		if !ok {
			return false
		}

		selector, ok := pointer.X.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "T" {
			return false
		}

		id, ok := selector.X.(*ast.Ident)

		return ok && id.Obj == nil && imports[fileOf(typ)][id.Name] == "testing"
	}

	var cliOffset func(ast.Expr, int) int

	cliOffset = func(expr ast.Expr, depth int) int {
		if depth > 10 {
			return -1
		}

		name, imported := "", ""

		switch fun := ast.Unparen(expr).(type) {
		case *ast.SelectorExpr:
			if id, ok := ast.Unparen(fun.X).(*ast.Ident); ok && id.Obj == nil {
				name, imported = fun.Sel.Name, imports[fileOf(fun)][id.Name]
			}
		case *ast.Ident:
			if fun.Obj == nil && functions[fun.Name] == nil {
				name, imported = fun.Name, imports[fileOf(fun)]["."]
			} else if ws := writes[fun.Obj]; len(ws) == 1 && ws[0].pos < fun.Pos() && ws[0].expr != nil {
				return cliOffset(ws[0].expr, depth+1)
			}
		}

		if imported == internalPrefix+"livetest" {
			switch name {
			case "CLI":
				return 3
			case "CLIIn":
				return 4
			}
		}

		return -1
	}

	var resolve func(ast.Expr) function

	resolve = func(expr ast.Expr) function {
		switch expr := expr.(type) {
		case *ast.FuncLit:
			return function{expr.Type.Params, expr.Body}
		case *ast.Ident:
			if expr.Obj == nil {
				if decl := functions[expr.Name]; decl != nil {
					return function{decl.Type.Params, decl.Body}
				}

				break
			}

			if decl, ok := expr.Obj.Decl.(*ast.FuncDecl); ok {
				return function{decl.Type.Params, decl.Body}
			}

			if ws := writes[expr.Obj]; len(ws) == 1 && ws[0].pos < expr.Pos() {
				if literal, ok := ws[0].expr.(*ast.FuncLit); ok {
					return resolve(literal)
				}
			}
		case *ast.SelectorExpr:
			// Only a receiver whose declared type or constructor result is local.
			id, ok := expr.X.(*ast.Ident)
			if !ok || id.Obj == nil {
				break
			}

			var typ ast.Expr
			if field, ok := id.Obj.Decl.(*ast.Field); ok {
				typ = field.Type
			} else if ws := writes[id.Obj]; len(ws) == 1 {
				switch init := ws[0].expr.(type) {
				case *ast.CompositeLit:
					typ = init.Type
				case *ast.CallExpr:
					name, named := init.Fun.(*ast.Ident)
					if !named {
						break
					}

					decl := functions[name.Name]
					if name.Obj != nil && name.Obj.Decl != decl {
						break
					}

					if decl != nil && decl.Type.Results != nil && len(decl.Type.Results.List) == 1 {
						typ = decl.Type.Results.List[0].Type
					}
				}
			}

			if decl := functions[fmt.Sprint(typ)+"."+expr.Sel.Name]; decl != nil {
				return function{decl.Type.Params, decl.Body}
			}
		}

		return function{}
	}

	var head func(ast.Expr, token.Pos, map[*liveLexicalObject]liveHead, int) liveHead

	head = func(expr ast.Expr, before token.Pos, bound map[*liveLexicalObject]liveHead, depth int) liveHead {
		if depth > 40 {
			return liveHead{}
		}

		next := func(expr ast.Expr) liveHead { return head(expr, before, bound, depth+1) }
		switch expr := expr.(type) {
		case *ast.ParenExpr:
			return next(expr.X)
		case *ast.BasicLit:
			if expr.Kind == token.STRING {
				word, _ := strconv.Unquote(expr.Value)

				return liveHead{word: word}
			}
		case *ast.BinaryExpr:
			left, right := next(expr.X), next(expr.Y)
			if expr.Op == token.ADD && left.word != "" && right.word != "" {
				return liveHead{word: left.word + right.word}
			}
		case *ast.CompositeLit:
			if array, ok := expr.Type.(*ast.ArrayType); ok && array.Len == nil {
				if id, ok := array.Elt.(*ast.Ident); ok && id.Name == "string" {
					if len(expr.Elts) == 0 {
						return liveHead{empty: true}
					}

					return next(expr.Elts[0])
				}
			}
		case *ast.Ident:
			ws := writes[expr.Obj]
			for i := len(ws) - 1; i >= 0; i-- {
				if ws[i].pos >= before {
					continue
				}

				value := head(ws[i].expr, ws[i].pos, bound, depth+1)

				within := ws[i].block == nil
				for node := ast.Node(expr); node != nil; node = parents[node] {
					within = within || node == ws[i].block
				}

				if !within {
					// A conditional/loop tail append is safe only when the
					// prefix was already known before entering that block.
					prior := head(expr, ws[i].pos, bound, depth+1)
					if value.word == "" || value != prior {
						return liveHead{}
					}
				}

				return value
			}

			return bound[expr.Obj]
		case *ast.CallExpr:
			id, _ := expr.Fun.(*ast.Ident)

			builtin := id != nil && id.Obj == nil && functions[id.Name] == nil
			if builtin && id.Name == "append" && len(expr.Args) > 1 {
				prefix := next(expr.Args[0])
				if prefix.empty {
					return next(expr.Args[1])
				}

				return prefix
			}

			if builtin && id.Name == "make" && len(expr.Args) >= 2 {
				array, ok := expr.Args[0].(*ast.ArrayType)
				if !ok || array.Len != nil {
					break
				}

				elt, ok := array.Elt.(*ast.Ident)
				if !ok || elt.Name != "string" {
					break
				}

				if length, ok := expr.Args[1].(*ast.BasicLit); ok && length.Value == "0" {
					return liveHead{empty: true}
				}
			}

			fn := resolve(expr.Fun)
			if fn.body != nil {
				var returns []*ast.ReturnStmt

				ast.Inspect(fn.body, func(node ast.Node) bool {
					if _, ok := node.(*ast.FuncLit); ok {
						return false
					}

					if ret, ok := node.(*ast.ReturnStmt); ok {
						returns = append(returns, ret)
					}

					return true
				})

				if len(returns) == 1 && len(returns[0].Results) == 1 {
					return head(returns[0].Results[0], returns[0].Pos(), liveBind(fn.params, expr, next, bound), depth+1)
				}
			}
		}

		return liveHead{}
	}
	covered := map[string][]string{}
	unknown := map[string]bool{}
	active := map[*ast.BlockStmt]bool{}

	var scan func(*ast.BlockStmt, map[*liveLexicalObject]liveHead)

	scan = func(body *ast.BlockStmt, bound map[*liveLexicalObject]liveHead) {
		if body == nil || active[body] {
			return
		}

		active[body] = true
		defer delete(active, body)

		ast.Inspect(body, func(node ast.Node) bool {
			if _, ok := node.(*ast.FuncLit); ok {
				return false // A declaration is not an invocation.
			}

			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}

			eval := func(expr ast.Expr) liveHead { return head(expr, call.Pos(), bound, 0) }

			if offset := cliOffset(call.Fun, 0); offset >= 0 {
				location := fset.Position(call.Pos()).String()

				var prefix liveHead
				if len(call.Args) > offset {
					prefix = eval(call.Args[offset])
				}

				if prefix.word == "" {
					unknown[location] = true
				} else if !slices.Contains(covered[prefix.word], location) {
					covered[prefix.word] = append(covered[prefix.word], location)
				}
			} else if fn := resolve(call.Fun); fn.body != nil {
				scan(fn.body, liveBind(fn.params, call, eval, bound))
			}

			method, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (method.Sel.Name != "Run" && method.Sel.Name != "Cleanup") {
				return true
			}

			receiver, ok := method.X.(*ast.Ident)
			if !ok || receiver.Obj == nil {
				return true
			}

			field, ok := receiver.Obj.Decl.(*ast.Field)
			if !ok || !testingParam(field.Type) {
				return true
			}

			for _, arg := range call.Args {
				if callback, ok := arg.(*ast.FuncLit); ok {
					scan(callback.Body, bound)
				}
			}

			return true
		})
	}

	for name, decl := range functions {
		if strings.HasPrefix(name, "Test") && (len(name) == 4 || !unicode.IsLower([]rune(name)[4])) && decl.Recv == nil &&
			decl.Type.Results == nil && decl.Type.Params != nil && decl.Type.Params.NumFields() == 1 && testingParam(decl.Type.Params.List[0].Type) {
			scan(decl.Body, nil)
		}
	}

	unresolved := make([]string, 0, len(unknown))
	for location := range unknown {
		unresolved = append(unresolved, location)
	}

	slices.Sort(unresolved)

	for _, locations := range covered {
		slices.Sort(locations)
	}

	return covered, unresolved
}

func liveBind(params *ast.FieldList, call *ast.CallExpr, eval func(ast.Expr) liveHead, outer map[*liveLexicalObject]liveHead) map[*liveLexicalObject]liveHead {
	bound := map[*liveLexicalObject]liveHead{}
	for obj, value := range outer {
		bound[obj] = value
	}

	i := 0

	if params != nil {
		for _, field := range params.List {
			for _, name := range field.Names {
				if i < len(call.Args) {
					bound[name.Obj] = eval(call.Args[i])
				}

				i++
			}
		}
	}

	return bound
}
