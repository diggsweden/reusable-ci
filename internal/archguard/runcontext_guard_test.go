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
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// insideLayers are the layers that must never read a run-context variable
// from the environment.
//
// They are the INSIDE of the hexagon: they are handed what they need and do
// not go looking for it. domain has always been at zero; app reached zero
// once the four --temp-dir flags were threaded.
//
// adapters and cli are deliberately absent, and that is not laziness:
//
//   - An adapter IS a forge's dialect. adapters/github/context.go reading
//     $GITHUB_SHA, or adapters/gitlab reading $CI_SERVER_URL, is that
//     adapter doing its job -- normalizing one forge's names into a neutral
//     Context. cienv's EventName doc turns on exactly this: GitLab's
//     CI_PIPELINE_SOURCE is kept OUT of the flag chain precisely so the
//     gitlab adapter can normalize its dialect. A rule forbidding those
//     reads would forbid the provider-adapter design itself.
//   - cli is where the environment is SUPPOSED to be read -- through flag
//     Sources. Its reads are the composition root doing its job.
//
// The failure this catches is narrower and real: an inside-layer read
// resolves ONE name, so it cannot honour the precedence its own flag
// already implements. Every such read found in this codebase was blind to
// $CI_TEMP_DIR while the flag beside it honoured it.
func insideLayers() []string { return []string{"domain", "app"} }

// ownedNames returns every environment variable runcontext defines,
// derived from runcontext.All() rather than restated here -- a hand-copied
// list would be a second source of truth, which is the failure runcontext
// exists to end.
func ownedNames() map[string]string {
	owned := make(map[string]string)

	for _, v := range runcontext.All() {
		for _, name := range v.Names {
			owned[name] = v.Concept
		}
	}

	return owned
}

// TestInsideLayersDoNotReadRunContextEnv fails when domain or app resolves a
// run-context variable itself instead of receiving it.
//
// The fix is always the same, and the tree already demonstrates it: declare
// a flag whose Sources is the matching cienv chain, carry the value on the
// use case's *Input, and delete the read. See app/build/xcodesigning.go's
// TempDir field for the shape.
func TestInsideLayersDoNotReadRunContextEnv(t *testing.T) {
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

		if !slicesContains(insideLayers(), layerOf(filepath.ToSlash(filepath.Dir(rel)))) {
			return nil
		}

		reportEnvReads(t, path, owned)

		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/: %v", err)
	}
}

// reportEnvReads flags each os.Getenv/os.LookupEnv in path whose argument is
// a run-context variable name.
func reportEnvReads(t *testing.T, path string, owned map[string]string) {
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
				"    An os.Getenv here resolves that ONE name, so it cannot honour the\n"+
				"    precedence the matching cienv chain implements (%s) -- the run would\n"+
				"    answer differently here than at every flag.\n"+
				"    Fix: give the command a flag with Sources: cienv.<Concept>(), carry the\n"+
				"    value on the use case's *Input, and delete the read.",
			fset.Position(n.Pos()), name, name, concept, conceptNames(name, owned),
		)

		return true
	})
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

func slicesContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}

	return false
}
