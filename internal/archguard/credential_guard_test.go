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

// forgeSpanningChains are the runcontext helpers whose name lists cross
// forges: CI_TOKEN, FORGEJO_TOKEN, GITEA_TOKEN, GITHUB_TOKEN.
//
// They are safe where they are used today and nowhere else:
//
//   - cienv binds them to --token / --release-token, where the caller has
//     explicitly said "use this token for what I asked for";
//   - validate/prerequisites asks whether a credential EXISTS, and never
//     transmits it.
//
// An adapter is the opposite case. It knows exactly which host it is about
// to call, so a chain that answers "whichever forge's token happens to be
// set" can hand it a credential issued by a different one.
var forgeSpanningChains = map[string]bool{ //nolint:gochecknoglobals // guard fixture.
	"Token":        true,
	"ReleaseToken": true,
}

// TestAdaptersDoNotResolveForgeSpanningTokens fails when an adapter obtains
// a credential from a chain that spans forges.
//
// This is a confused-deputy guard. An adapter holds authority (a token) and
// a destination (its forge's API). If it resolves the credential from a
// chain naming several forges' variables, a run that sets more than one --
// a job publishing across forges -- can talk it into presenting forge A's
// credential to forge B. A cross-forge token cannot authenticate anyway, so
// the only two outcomes are an auth failure or a disclosure.
//
// This was not hypothetical: the forgejo package-registry resolver used
// runcontext.Token(), whose chain ends in GITHUB_TOKEN. On a GitHub runner
// publishing to a third-party Forgejo instance with FORGEJO_TOKEN unset (an
// unset ${{ secrets.X }} interpolates to "", and empty means absent, so it
// fell through), it transmitted the GitHub job token to that host.
//
// The fix an offender should reach for is a forge-scoped resolver --
// runcontext.TokenForForgejo, which admits $GITHUB_TOKEN only when a Forgejo
// runner injected it under the GitHub-compatible name -- or simply naming
// its own forge's variable, as the github adapter does.
func TestAdaptersDoNotResolveForgeSpanningTokens(t *testing.T) {
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

		if layerOf(filepath.ToSlash(filepath.Dir(rel))) != "adapters" {
			return nil
		}

		reportSpanningTokenUse(t, path)

		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/: %v", err)
	}
}

// reportSpanningTokenUse flags each runcontext.Token()/ReleaseToken() call
// in path.
func reportSpanningTokenUse(t *testing.T, path string) {
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
		if !ok || pkg.Name != "runcontext" || !forgeSpanningChains[sel.Sel.Name] {
			return true
		}

		t.Errorf(
			"%s: resolves a credential from runcontext.%s(), whose chain spans forges.\n"+
				"    An adapter knows which host it is about to call, so this can hand it a\n"+
				"    token issued by a DIFFERENT forge -- which cannot authenticate there, but\n"+
				"    would be transmitted there. That is a credential disclosure, not a bug in\n"+
				"    the happy path.\n"+
				"    Fix: use a forge-scoped resolver (runcontext.TokenForForgejo), or name your\n"+
				"    own forge's variable directly as the github adapter does.",
			fset.Position(n.Pos()), sel.Sel.Name,
		)

		return true
	})
}
