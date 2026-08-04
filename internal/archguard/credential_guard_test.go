// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archguard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
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

	root := filepath.Join("..", "..", "internal")

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
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

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "runcontext" || sel.Sel.Name != "OperatorCredential" {
			return true
		}

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
			fset.Position(n.Pos()),
		)

		return true
	})
}
