// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archguard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
)

// TestOnlyCompositionRootMintsOperatorCredentials fails when a package
// outside the CLI turns a bare string into an unrestricted credential.
//
// # What the compiler already covers
//
// runcontext.Token()/ReleaseToken() have name lists spanning forges, but they
// resolve to a runcontext.Credential, which has no method that yields the
// secret without being told where it is going. An adapter may call them freely
// -- the type will not let it send GitHub's token to Forgejo -- so this guard
// does not police that.
//
// What the compiler cannot see is runcontext.OperatorCredential, the single
// constructor that mints a Credential from an arbitrary string with NO
// audience. It has to exist: an operator who types `--token ghs_...` has
// decided where that secret goes, and no rule here can second-guess them.
// But it is also the one way to launder an ambient token past its audience --
//
//	runcontext.OperatorCredential(os.Getenv("GITHUB_TOKEN")).For(anywhere)
//
// -- which is precisely the disclosure the type exists to prevent. So the
// mint is confined to the composition root, where "the operator said so" is
// actually true because that is the layer holding the operator's input.
//
// internal/cli/clitoken is the intended home; the whole of internal/cli is
// allowed because the composition root is exempt from the run-context rules
// generally (see runcontext_guard_test.go) and drawing a finer line here
// would be a rule about one package rather than about layers.
func TestOnlyCompositionRootMintsOperatorCredentials(t *testing.T) {
	t.Parallel()

	root := filepath.Join(reporoot.Path(t), "internal")

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !isProductGoFile(d, path) {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		if layer := layerOf(filepath.ToSlash(filepath.Dir(rel))); layer == "cli" {
			return nil
		}

		reportOperatorCredentialMint(t, path)

		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/: %v", err)
	}
}

// reportOperatorCredentialMint flags each runcontext.OperatorCredential call
// in path.
func reportOperatorCredentialMint(t *testing.T, path string) {
	t.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	for _, position := range operatorCredentialReferences(file) {
		t.Errorf(
			"%s: mints an unrestricted credential from a string.\n"+
				"    OperatorCredential means \"an operator typed this secret next to the\n"+
				"    command it is for\", which only the composition root can know. Anywhere\n"+
				"    else it strips a credential of its audience and re-enables sending one\n"+
				"    forge's token to another -- the disclosure runcontext.Credential exists\n"+
				"    to make unwritable.\n"+
				"    Fix: resolve via runcontext.Token()/ReleaseToken(), which bind the\n"+
				"    credential to the server that issued it, and name your destination with\n"+
				"    Credential.For.",
			fset.Position(position),
		)
	}
}

// Forbid references, not just direct calls: a function value can be passed to
// reflection or another helper. Parser objects distinguish local shadowing.
func operatorCredentialReferences(file *ast.File) []token.Pos {
	names := map[string]bool{}
	dot := false

	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path != internalPrefix+"runcontext" {
			continue
		}

		name := "runcontext"
		if spec.Name != nil {
			name = spec.Name.Name
		}

		if name == "." {
			dot = true
		} else {
			names[name] = true
		}
	}

	var (
		found []token.Pos
		visit func(ast.Node) bool
	)

	visit = func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.SelectorExpr:
			if pkg, ok := node.X.(*ast.Ident); ok && pkg.Obj == nil && names[pkg.Name] && node.Sel.Name == "OperatorCredential" {
				found = append(found, node.Pos())
			}

			ast.Inspect(node.X, visit)

			return false
		case *ast.Ident:
			if dot && node.Obj == nil && node.Name == "OperatorCredential" {
				found = append(found, node.Pos())
			}
		}

		return true
	}
	ast.Inspect(file, visit)

	return found
}
