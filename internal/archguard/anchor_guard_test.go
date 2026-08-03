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

// descriptiveChains are the runcontext helpers that sort by "most deliberate
// wins": the bare $REPOSITORY / $CI_SERVER_URL the orchestration layer
// computes are preferred ahead of the runner's own values.
//
// That is the right order for describing a run and the wrong one for
// anchoring trust, which must sort by "least forgeable wins".
var descriptiveChains = map[string]bool{ //nolint:gochecknoglobals // guard fixture.
	"Repository":      true,
	"RepositoryOwner": true,
	"ServerURL":       true,
}

// TestKeylessIdentityAnchorsOnAttestedValues fails when ResolveKeylessIdentity
// resolves its anchor from a descriptive chain.
//
// SubjectRegexp becomes cosign's --certificate-identity-regexp, so it decides
// which certificates verification ACCEPTS. Widening where it comes from widens
// what a signature check will trust. ADR 0002's invariant is that nothing
// upstream can cause scope escalation inside the signing boundary, and rule 3
// shows the intended shape: an explicit --expected-image-repository,
// re-validated -- never a value inferred from a chain someone else computed.
//
// This is not hypothetical. The forgejo resolver was switched to
// runcontext.Repository() during a consistency pass, which silently falsified
// the reason the fallback is allowed to exist at all: validate's
// keylessVerifyIdentity documents the derived identity as "read from the
// trusted runner environment". Nothing caught it, because every other guard in
// this package enforces SAMENESS -- one chain per concept -- and this is the
// one place that needs difference.
//
// The remedy is a forge-scoped Attested* chain (runcontext.AttestedForgejo*),
// or naming the runner's own variable directly as the github and gitlab
// resolvers do.
func TestKeylessIdentityAnchorsOnAttestedValues(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "internal")

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		reportDescriptiveAnchors(t, path)

		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/: %v", err)
	}
}

// reportDescriptiveAnchors flags descriptive-chain calls inside any
// ResolveKeylessIdentity method in path.
func reportDescriptiveAnchors(t *testing.T, path string) {
	t.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "ResolveKeylessIdentity" || fn.Body == nil {
			continue
		}

		ast.Inspect(fn.Body, func(n ast.Node) bool {
			name, ok := descriptiveChainCall(n)
			if !ok {
				return true
			}

			t.Errorf(
				"%s: ResolveKeylessIdentity anchors on runcontext.%s().\n"+
					"    That chain prefers the bare $REPOSITORY / $CI_SERVER_URL the\n"+
					"    orchestration layer computes over the values the runner injected.\n"+
					"    SubjectRegexp becomes cosign's --certificate-identity-regexp, so this\n"+
					"    decides which certificates verification ACCEPTS: it must resolve from\n"+
					"    what the runner injected, which nothing upstream had to compute.\n"+
					"    Fix: use a forge-scoped runcontext.Attested* chain, or read the\n"+
					"    runner's own variable by name as the github/gitlab resolvers do.",
				fset.Position(n.Pos()), name,
			)

			return true
		})
	}
}

// descriptiveChainCall reports the helper name when n is a runcontext call
// onto a chain that is unsafe to anchor trust on.
func descriptiveChainCall(n ast.Node) (string, bool) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return "", false
	}

	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}

	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "runcontext" || !descriptiveChains[sel.Sel.Name] {
		return "", false
	}

	return sel.Sel.Name, true
}
