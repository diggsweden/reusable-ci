// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archguard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
)

// The rule is ADR 0004's: application logic asks the provider what it can do,
// it does not branch on which provider it is. Forge differences live behind the
// role interfaces, so adding a forge means writing an adapter, not adding a
// `case` arm across internal/app.
//
// The rule was enforced by searching each file for six literal strings:
// `case provider.ForgeGitHub`, `== provider.ForgeGitLab`, and four more. That
// list is a description of how the violation was written the last time someone
// wrote one. It missed:
//
//   - `!=`, which is how BOTH live violations were spelled. app/validate and
//     app/doctor each branched on `forge != provider.ForgeGitHub` and neither
//     guard run ever mentioned them.
//   - provider.ForgeLocal, a fourth constant the list simply never had.
//   - the operands the other way round, `provider.ForgeGitHub == forge`.
//   - a renamed or dot import, so `p.ForgeGitHub` or a bare `ForgeGitHub`.
//   - a constant bound to a local first, then compared.
//   - `switch { case forge == provider.ForgeGitHub: }`, whose case arm is a
//     comparison rather than the constant.
//   - any amount of whitespace other than one space.
//
// And it reported text in comments, which is the other half of why a substring
// guard erodes: the first false positive teaches people to phrase around it.
//
// This resolves the constants instead. It is import-aware rather than
// type-checked — the same bounded approach the credential guard uses — so it
// follows the provider package under whatever local name a file gives it, and a
// comment or a string containing the same words is not a comparison.
func TestAppLayerDoesNotBranchOnForgeIdentity(t *testing.T) {
	t.Parallel()

	root := reporoot.Path(t)
	appDir := filepath.Join(root, "internal", "app")

	var (
		offenders []string
		scanned   int
	)

	err := filepath.WalkDir(appDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if !isProductGoFile(entry, path) {
			return nil
		}

		fset := token.NewFileSet()

		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return fmt.Errorf("parse %s: %w", path, parseErr)
		}

		scanned++

		rel, _ := filepath.Rel(root, path)

		for _, finding := range forgeIdentityBranches(fset, file) {
			offenders = append(offenders, filepath.ToSlash(rel)+":"+finding)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if scanned < 50 {
		t.Fatalf("scanned %d app files; the walk, not the tree, is what was measured", scanned)
	}

	sort.Strings(offenders)

	if len(offenders) > 0 {
		t.Errorf("application code branches on a forge's identity:\n  %s\n\n"+
			"Ask the provider what it can do instead. If the question really is about a forge or an\n"+
			"instance, put the predicate in internal/domain/provider and call that — see\n"+
			"provider.ClassifyReleaseWorkflowScope, which is where two of these used to live.",
			strings.Join(offenders, "\n  "))
	}
}

// forgeIdentityConstants are every concrete provider.ForgeAPI value. All four,
// including ForgeLocal, which the substring list never had.
func forgeIdentityConstants() map[string]bool {
	return map[string]bool{
		"ForgeGitHub":  true,
		"ForgeGitLab":  true,
		"ForgeForgejo": true,
		"ForgeLocal":   true,
	}
}

const providerImportPath = "github.com/diggsweden/reusable-ci/v3/internal/domain/provider"

// forgeIdentityBranches reports every comparison or switch case in one file
// that tests a value against a concrete forge constant.
func forgeIdentityBranches(fset *token.FileSet, file *ast.File) []string {
	local, imported := providerLocalName(file)
	if !imported {
		return nil
	}

	names := forgeIdentityConstants()

	// A constant bound to a local first is the same branch one step removed.
	aliases := map[string]bool{}

	ast.Inspect(file, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != len(assign.Rhs) {
			return true
		}

		for i, lhs := range assign.Lhs {
			ident, isIdent := lhs.(*ast.Ident)
			if isIdent && namesForgeConstant(assign.Rhs[i], local, names, nil) {
				aliases[ident.Name] = true
			}
		}

		return true
	})

	var findings []string

	report := func(pos token.Pos, form string) {
		findings = append(findings, fmt.Sprintf("%d: %s", fset.Position(pos).Line, form))
	}

	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.BinaryExpr:
			if node.Op != token.EQL && node.Op != token.NEQ {
				return true
			}

			if namesForgeConstant(node.X, local, names, aliases) || namesForgeConstant(node.Y, local, names, aliases) {
				report(node.Pos(), "comparison against a forge constant")
			}
		case *ast.CaseClause:
			for _, expr := range node.List {
				if namesForgeConstant(expr, local, names, aliases) {
					report(expr.Pos(), "switch case on a forge constant")
				}
			}
		}

		return true
	})

	return findings
}

// namesForgeConstant reports whether expr denotes a concrete forge constant,
// under the provider package's local name in this file, as a bare identifier
// when the package is dot-imported, or through a local bound to one.
func namesForgeConstant(expr ast.Expr, local string, names, aliases map[string]bool) bool {
	switch expr := ast.Unparen(expr).(type) {
	case *ast.SelectorExpr:
		pkg, ok := expr.X.(*ast.Ident)

		return ok && pkg.Name == local && names[expr.Sel.Name]
	case *ast.Ident:
		// Dot import, or a local bound to a constant earlier in the file.
		return (local == "." && names[expr.Name]) || aliases[expr.Name]
	default:
		return false
	}
}

// providerLocalName returns the name the provider package is bound to in this
// file: its own name, a rename, or "." for a dot import.
func providerLocalName(file *ast.File) (string, bool) {
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path != providerImportPath {
			continue
		}

		if spec.Name != nil {
			return spec.Name.Name, true
		}

		return "provider", true
	}

	return "", false
}
