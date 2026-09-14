// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package lexiconguard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
	"github.com/stretchr/testify/require"
)

// TestRetryBackoffIsSingleSourced detects a bounded, structural retry idiom:
// a loop exits on a nil, short-declared call result, then waits before trying
// again. The wait's arithmetic spelling is irrelevant, so reordered products,
// intermediate delay variables and constant waits are covered without flagging
// Duration conversions.
// App/adapter retries should use retry.Do/Run instead.
//
// This is not a control-flow/type analyzer: delegated waits in helpers,
// computed function-value aliases, non-nil success predicates and success
// exits hidden in helpers are outside this rule; direct aliases of a wait,
// reassigned results and else-branch waits are recognised (see
// TestRetryWaitGuardSyntax). Canonical calls do not exempt other code in the
// same file.
const retryOwner = "internal/retry/retry.go"

func TestRetryBackoffIsSingleSourced(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	poison := "package fixture\nimport clock \"time\"\nfunc f() { for { if err := op(); err == nil { return }; clock.Sleep(delay) } }\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "poison.go"), []byte(poison), 0o600))

	// The owner is exempt because it holds the canonical wait; an exemption
	// for a file that no longer waits would hide nothing and guard nothing.
	owner, err := parser.ParseFile(token.NewFileSet(), filepath.Join(reporoot.Path(t), retryOwner), nil, 0)
	require.NoError(t, err)
	require.NotEmpty(t, retryWaits(owner), "%s holds no retry wait, so its exemption is stale", retryOwner)

	for _, tc := range []struct {
		name string
		root string
		want []string
	}{
		{"repository", reporoot.Path(t), nil},
		{"poison", root, []string{"poison.go:3"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var findings []string

			visited, err := walkGoSources(tc.root, func(path, rel string) error {
				if rel == retryOwner {
					return nil
				}

				fset := token.NewFileSet()

				file, err := parser.ParseFile(fset, path, nil, 0)
				if err != nil {
					return err
				}

				for _, pos := range retryWaits(file) {
					findings = append(findings, fmt.Sprintf("%s:%d", rel, fset.Position(pos).Line))
				}

				return nil
			})
			require.NoError(t, err)
			require.NotZero(t, visited)
			require.Equal(t, tc.want, findings, "local retry wait outside internal/retry; use retry.Do/Run")
		})
	}
}

func retryWaits(file *ast.File) []token.Pos {
	var found []token.Pos

	ast.Inspect(file, func(node ast.Node) bool {
		var body *ast.BlockStmt

		switch loop := node.(type) {
		case *ast.ForStmt:
			body = loop.Body
		case *ast.RangeStmt:
			body = loop.Body
		default:
			return true
		}

		retrying := false

		for _, stmt := range body.List {
			if guard, ok := stmt.(*ast.IfStmt); ok && nilResultExit(guard, body) {
				retrying = true

				// A wait in the guard's else branch is on the retry path just
				// as much as one after the guard. Written as if/else it reads
				// naturally and used to be invisible here.
				if guard.Else != nil {
					found = append(found, waitsIn(file, guard.Else, body.Pos())...)
				}

				continue
			}

			if !retrying {
				continue
			}

			found = append(found, waitsIn(file, stmt, body.Pos())...)
		}

		return true
	})

	return found
}

// waitsIn collects the retry waits inside one statement, using the same rules
// as the scan over a loop body's later statements.
func waitsIn(file *ast.File, stmt ast.Stmt, loopStart token.Pos) []token.Pos {
	var found []token.Pos

	ast.Inspect(stmt, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt, *ast.ForStmt, *ast.RangeStmt:
			return false
		case *ast.CallExpr:
			if importedFunction(file, node.Fun, "time", "Sleep") || aliasOf(file, node.Fun, "time", "Sleep") {
				found = append(found, node.Pos())
			}
		case *ast.UnaryExpr:
			if node.Op == token.ARROW && timerReceive(file, node.X, loopStart) {
				found = append(found, node.Pos())
			}
		default:
		}

		return true
	})

	return found
}

// aliasOf resolves a wait function bound to a local first — `sleep :=
// time.Sleep` and then `sleep(d)`. The binding must be a single assignment from
// the imported selector itself; anything computed is not followed, because a
// guess about what a variable holds is worse than declining to say.
func aliasOf(file *ast.File, fun ast.Expr, pkg, name string) bool {
	ident, ok := ast.Unparen(fun).(*ast.Ident)
	if !ok || ident.Obj == nil {
		return false
	}

	assignment, ok := ident.Obj.Decl.(*ast.AssignStmt)
	if !ok || len(assignment.Rhs) != 1 || len(assignment.Lhs) != 1 {
		return false
	}

	return importedFunction(file, ast.Unparen(assignment.Rhs[0]), pkg, name)
}

// resultAssignedFromCallInLoop reports whether the named result is assigned the
// value of a call somewhere inside this loop. It is what makes a result
// declared BEFORE the loop — `var err error`, then `err = op()` inside —
// count as a retry result, which a declaration-site-only rule could not see.
func resultAssignedFromCallInLoop(body *ast.BlockStmt, result *ast.Ident) bool {
	assigned := false

	ast.Inspect(body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok || len(assignment.Rhs) != 1 {
			return true
		}

		if _, isCall := ast.Unparen(assignment.Rhs[0]).(*ast.CallExpr); !isCall {
			return true
		}

		for _, lhs := range assignment.Lhs {
			if ident, isIdent := ast.Unparen(lhs).(*ast.Ident); isIdent && ident.Obj == result.Obj {
				assigned = true
			}
		}

		return true
	})

	return assigned
}

func nilResultExit(guard *ast.IfStmt, body *ast.BlockStmt) bool {
	loopStart := body.Pos()

	cond, ok := ast.Unparen(guard.Cond).(*ast.BinaryExpr)
	if !ok || cond.Op != token.EQL {
		return false
	}

	result, leftOK := ast.Unparen(cond.X).(*ast.Ident)

	nilID, rightOK := ast.Unparen(cond.Y).(*ast.Ident)
	if !leftOK || !rightOK {
		return false
	}

	if result.Name == "nil" && result.Obj == nil {
		result, nilID = nilID, result
	}

	if nilID.Name != "nil" || nilID.Obj != nil || result.Obj == nil {
		return false
	}

	switch decl := result.Obj.Decl.(type) {
	case *ast.AssignStmt:
		if len(decl.Rhs) != 1 || decl.Pos() < loopStart {
			// Declared before the loop. It is still a retry result if the loop
			// reassigns it from a call each turn.
			if !resultAssignedFromCallInLoop(body, result) {
				return false
			}

			break
		}

		if _, isCall := ast.Unparen(decl.Rhs[0]).(*ast.CallExpr); !isCall {
			return false
		}
	case *ast.ValueSpec:
		// `var err error` above the loop, assigned inside it.
		if !resultAssignedFromCallInLoop(body, result) {
			return false
		}
	default:
		return false
	}

	for _, stmt := range guard.Body.List {
		switch exit := stmt.(type) {
		case *ast.ReturnStmt:
			return true
		case *ast.BranchStmt:
			if exit.Tok == token.BREAK && exit.Label == nil {
				return true
			}
		default:
		}
	}

	return false
}

func timerReceive(file *ast.File, expr ast.Expr, loopStart token.Pos) bool {
	expr = ast.Unparen(expr)
	if sel, ok := expr.(*ast.SelectorExpr); ok && sel.Sel.Name == "C" {
		id, ok := sel.X.(*ast.Ident)
		if !ok || id.Obj == nil {
			return false
		}

		assignment, ok := id.Obj.Decl.(*ast.AssignStmt)
		if !ok || len(assignment.Rhs) != 1 || len(assignment.Lhs) != 1 || assignment.Pos() < loopStart {
			return false
		}

		call, ok := ast.Unparen(assignment.Rhs[0]).(*ast.CallExpr)

		return ok && importedFunction(file, call.Fun, "time", "NewTimer")
	}

	call, ok := expr.(*ast.CallExpr)

	return ok && importedFunction(file, call.Fun, "time", "After")
}
