// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archguard

import (
	"go/ast"
	"go/parser"
	"go/types"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

type envSemanticImporter struct {
	types.Importer
	keys *types.Package
}

func (i envSemanticImporter) Import(path string) (*types.Package, error) {
	if path == i.keys.Path() {
		return i.keys, nil
	}

	return i.Importer.Import(path)
}

func TestRunContextEnvSemanticFixtures(t *testing.T) {
	t.Parallel()
	_, imp, fset := envGuardPackages(t, "os", "./internal/runcontext")
	// A real type-checked imported package, with a package name unlike its path.
	// No fixture function or environment source is ever executed.
	keysFile, err := parser.ParseFile(fset, "keys.go", `package names
const Prefix = "GITHUB_"
const Commit = Prefix + "SHA"
var Getenv = func(string) string { return "" }
`, 0)
	require.NoError(t, err)

	config := types.Config{}
	keys, err := config.Check("fixture/keys", fset, []*ast.File{keysFile}, nil)
	require.NoError(t, err)

	imp = envSemanticImporter{Importer: imp, keys: keys}

	for _, tc := range []struct {
		name, source, sibling string
		want                  []string
	}{
		{name: "direct", source: `import "os"; var _ = os.Getenv("GITHUB_SHA")`, want: []string{"GITHUB_SHA"}},
		{name: "lookup alias", source: `import env "os"; var _, _ = env.LookupEnv("CI_COMMIT_SHA")`, want: []string{"CI_COMMIT_SHA"}},
		{name: "dot import", source: `import . "os"; var _ = Getenv("REPOSITORY")`, want: []string{"REPOSITORY"}},
		{name: "parenthesized", source: `import env "os"; var _ = (env.Getenv)(("GITHUB_SHA"))`, want: []string{"GITHUB_SHA"}},
		{name: "escaped literal", source: `import "os"; var _ = os.Getenv("\x47ITHUB_SHA")`, want: []string{"GITHUB_SHA"}},
		{name: "concatenation", source: `import "os"; var _ = os.Getenv("GITHUB_" + "SHA")`, want: []string{"GITHUB_SHA"}},
		{name: "typed local constants", source: `import "os"; func f() { type key string; const prefix key = "GITHUB_"; const name = string(prefix) + "SHA"; _ = os.Getenv(name) }`, want: []string{"GITHUB_SHA"}},
		{name: "sibling constant", source: `import "os"; var _ = os.Getenv(commit)`, sibling: `const prefix = "GITHUB_"; const commit = prefix + "SHA"`, want: []string{"GITHUB_SHA"}},
		{name: "imported constant", source: `import "os"; import "fixture/keys"; var _ = os.Getenv(names.Commit)`, want: []string{"GITHUB_SHA"}},
		{name: "imported alias constant", source: `import env "os"; import k "fixture/keys"; const name = k.Prefix + "SHA"; var _ = env.Getenv(name)`, want: []string{"GITHUB_SHA"}},
		{name: "imported dot constant", source: `import "os"; import . "fixture/keys"; var _ = os.Getenv(Commit)`, want: []string{"GITHUB_SHA"}},
		{name: "package function value", source: `import "os"; var read = os.Getenv; var _ = read("GITHUB_SHA")`, want: []string{"GITHUB_SHA"}},
		{name: "sibling function value", source: `var _ = read(commit)`, sibling: `import "os"; const commit = "GITHUB_SHA"; var read = os.Getenv`, want: []string{"GITHUB_SHA"}},
		{name: "local function chain", source: `import env "os"; func f() { read := (env.Getenv); next := read; _ = next("GITHUB_SHA") }`, want: []string{"GITHUB_SHA"}},
		{name: "assigned function", source: `import "os"; func f() { var read func(string) string; read = os.Getenv; _ = read("GITHUB_SHA") }`, want: []string{"GITHUB_SHA"}},
		{name: "lookup function value", source: `import "os"; func f() { read := os.LookupEnv; _, _ = read("GITHUB_SHA") }`, want: []string{"GITHUB_SHA"}},
		{name: "dot function value", source: `import . "os"; func f() { read := Getenv; _ = read("GITHUB_SHA") }`, want: []string{"GITHUB_SHA"}},
		{name: "parallel binding", source: `import "os"; func f() { read, n := os.Getenv, 1; _ = n; _ = read("GITHUB_SHA") }`, want: []string{"GITHUB_SHA"}},
		{name: "injected resolve", source: `import "os"; import rc "github.com/diggsweden/reusable-ci/v3/internal/runcontext"; var _ = rc.Repository().Resolve(os.Getenv); func f(env func(string)string) string { return rc.Repository().Resolve(env) }`},
		{name: "injected lookup", source: `func f(Getenv func(string)string) string { return Getenv("GITHUB_SHA") }`},
		{name: "non-owned names", source: `import "os"; var _ = os.Getenv("SOURCE_DATE_EPOCH"); var _, _ = os.LookupEnv("DOCKER_CONFIG")`},
		{name: "inert text", source: "var _ = `os.Getenv(\"GITHUB_SHA\")` // os.LookupEnv(\"GITHUB_SHA\")"},
		{name: "shadowed package parameter", source: `import "os"; var _ = os.Args; func f(os struct{ Getenv func(string)string }) { _ = os.Getenv("GITHUB_SHA") }`},
		{name: "shadowed alias local", source: `import env "os"; var _ = env.Args; func f() { env := struct{ LookupEnv func(string)(string,bool) }{}; _, _ = env.LookupEnv("GITHUB_SHA") }`},
		{name: "shadowed dot function", source: `import . "os"; var _ = Args; func f() { Getenv := func(string)string {return ""}; _ = Getenv("GITHUB_SHA") }`},
		{name: "unrelated method", source: `type fake struct{}; func (fake) Getenv(string) string { return "" }; var os fake; var _ = os.Getenv("GITHUB_SHA")`},
		{name: "other package named os", source: `import os "fixture/keys"; var _ = os.Getenv("GITHUB_SHA")`},
		{name: "shadowed constant", source: `import "os"; const key = "GITHUB_SHA"; func f() { const key = "DOCKER_CONFIG"; _ = os.Getenv(key) }`},
		{name: "shadowed function value", source: `import "os"; var read = os.Getenv; func f() { read := func(string)string{return ""}; _ = read("GITHUB_SHA") }`},
		{name: "shadow ends", source: `import "os"; func f() { read := os.Getenv; { read := func(string)string{return ""}; _ = read("GITHUB_SHA") }; _ = read("CI_COMMIT_SHA") }`, want: []string{"CI_COMMIT_SHA"}},
		// Explicit limits: these are not claims that runtime behavior is safe.
		{name: "limit runtime key", source: `import "os"; func f(key string) { _ = os.Getenv(key) }`},
		{name: "limit variable key", source: `import "os"; var key = "GITHUB_SHA"; var _ = os.Getenv(key)`},
		{name: "limit wrapper", source: `import "os"; func read(key string) string {return os.Getenv(key)}; var _ = read("GITHUB_SHA")`},
		{name: "limit reassignment", source: `import "os"; func f() { read := os.Getenv; read = func(string)string{return ""}; _ = read("GITHUB_SHA") }`},
		{name: "limit reassignment to stdlib", source: `import "os"; func f() { read := func(string)string{return ""}; read = os.Getenv; _ = read("GITHUB_SHA") }`},
		{name: "before local assignment", source: `import "os"; func f() { var read func(string)string; _ = read("GITHUB_SHA"); read = os.Getenv; _ = read }`},
		{name: "limit assigned parameter", source: `import "os"; func f(read func(string)string) { read = os.Getenv; _ = read("GITHUB_SHA") }`},
		{name: "limit addressed variable", source: `import "os"; func f() { read := os.Getenv; _ = &read; _ = read("GITHUB_SHA") }`},
		{name: "limit field", source: `import "os"; var holder = struct{ read func(string)string }{os.Getenv}; var _ = holder.read("GITHUB_SHA")`},
		{name: "limit container", source: `import "os"; var readers = []func(string)string{os.Getenv}; var _ = readers[0]("GITHUB_SHA")`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := make([]*ast.File, 0, 2)

			for i, source := range []string{tc.source, tc.sibling} {
				if source == "" {
					continue
				}

				file, err := parser.ParseFile(fset, []string{"fixture.go", "sibling.go"}[i], "package fixture; "+source, 0)
				require.NoError(t, err)

				files = append(files, file)
			}

			reads, err := runContextEnvReads(fset, "fixture/consumer", files, imp)
			require.NoError(t, err, "fixtures must type-check; errors are not successful refusals")

			var names []string //nolint:prealloc // preserve nil for no-read fixtures, distinguishing absence from an unexpected result.
			for _, read := range reads {
				names = append(names, read.name)
			}

			require.Equal(t, tc.want, names)
		})
	}

	t.Run("diagnostic location", func(t *testing.T) {
		file, err := parser.ParseFile(fset, "location.go", "package fixture\nimport env \"os\"\nfunc f() {\n const key = \"GITHUB_\" + \"SHA\"\n _ = env.Getenv(key)\n}\n", 0)
		require.NoError(t, err)
		reads, err := runContextEnvReads(fset, "fixture/location", []*ast.File{file}, imp)
		require.NoError(t, err)
		require.Len(t, reads, 1)
		pos := fset.Position(reads[0].pos)
		require.Equal(t, "location.go:5:6", pos.String())
		require.Equal(t, "GITHUB_SHA", reads[0].name)
	})
	t.Run("type errors are not a clean scan", func(t *testing.T) {
		file, err := parser.ParseFile(fset, "invalid.go", `package fixture; import "os"; var _ = os.Getenv(missing)`, 0)
		require.NoError(t, err)
		_, err = runContextEnvReads(fset, "fixture/invalid", []*ast.File{file}, imp)
		require.Error(t, err)
	})
}

func TestRunContextEnvGuardLayers(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		path string
		want bool
	}{
		{"domain/release", true}, {"app/build", true}, {"cli/cienv", true},
		{"neutralhelper", true}, {"runcontext", true}, {"cliio", true},
		{"adapters/github", false}, {"testutil/fixture", false}, {"livetest", false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			require.Equal(t, tc.want, slices.Contains(guardedLayers(), layerOf(tc.path)))
		})
	}

	require.Contains(t, runContextFix("utility"), "injected lookup")
	require.Contains(t, runContextFix("cli"), "Resolve(os.Getenv)")
	require.Contains(t, runContextFix("app"), "*Input")
}
