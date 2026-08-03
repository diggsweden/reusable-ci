// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archguard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// guardedLayers are the layers that must not name a run-context variable
// themselves. The rule they share:
//
//	Only an adapter may name a forge's variables, because an adapter IS a
//	dialect. Everyone else resolves the shared chain.
//
// adapters is therefore the ONE exemption, and it is principled rather than
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
func guardedLayers() []string { return []string{"domain", "app", "cli"} }

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

		layer := layerOf(filepath.ToSlash(filepath.Dir(rel)))
		if !slices.Contains(guardedLayers(), layer) {
			return nil
		}

		reportEnvReads(t, path, layer, owned)

		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/: %v", err)
	}
}

// reportEnvReads flags each os.Getenv/os.LookupEnv in path whose argument is
// a run-context variable name.
func reportEnvReads(t *testing.T, path, layer string, owned map[string]string) {
	t.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	ast.Inspect(file, func(n ast.Node) bool {
		name, ok := envReadArg(n)
		if !ok {
			return true
		}

		concept, owns := owned[name]
		if !owns {
			// Not a run-context variable. SOURCE_DATE_EPOCH and
			// DOCKER_CONFIG are read by app code and are not this
			// guard's business: they name a build input or a tool's
			// own config, not the CI run.
			return true
		}

		t.Errorf(
			"%s: reads $%s directly.\n"+
				"    $%s is the %q run context, owned by internal/runcontext.\n"+
				"    This read resolves that ONE name, so it cannot honour the precedence\n"+
				"    the shared chain implements (%s) --\n"+
				"    the same run would answer differently here than at every flag.\n"+
				"    Fix: %s",
			fset.Position(n.Pos()), name, name, concept, conceptNames(name, owned), runContextFix(layer),
		)

		return true
	})
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

	return "this layer receives the run context, it does not look for it. Give the\n" +
		"    command a flag with Sources: cienv.<Concept>(), carry the value on the use\n" +
		"    case's *Input, and delete the read."
}

// envReadArg reports the literal variable name when n is an os.Getenv or
// os.LookupEnv call on a constant string.
func envReadArg(n ast.Node) (string, bool) {
	call, ok := n.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return "", false
	}

	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}

	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "os" {
		return "", false
	}

	if sel.Sel.Name != "Getenv" && sel.Sel.Name != "LookupEnv" {
		return "", false
	}

	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}

	name, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}

	return name, true
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
