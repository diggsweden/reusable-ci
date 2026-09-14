// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archguard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// These are parsed source fixtures, never compiled or executed as live tests.
// '@' marks the exact call position expected in coverage or unresolved output.
func TestLiveParityInvocationFixtures(t *testing.T) {
	t.Parallel()

	const imported = `import "github.com/diggsweden/reusable-ci/v3/internal/livetest"` + "\n"
	for _, tc := range []struct {
		name, source, group string
		unresolved          bool
	}{
		{"direct", imported + `func TestCase(t *T) { @livetest.CLI(t, target, repo, "release", "version") }`, "release", false},
		{"CLIIn options are not argv", imported + `func TestCase(t *T) { @livetest.CLIIn(t, target, "version", opts, "container", "version") }`, "container", false},
		{"import alias", `import live "github.com/diggsweden/reusable-ci/v3/internal/livetest"
func TestCase(t *T) { @live.CLI(t, target, repo, "artifact") }`, "artifact", false},
		{"function alias", imported + `func TestCase(t *T) { invoke := livetest.CLI; @invoke(t, target, repo, "version") }`, "version", false},
		{"parenthesized function", imported + `func TestCase(t *T) { @(livetest.CLI)(t, target, repo, "version") }`, "version", false},
		{"dot import", `import . "github.com/diggsweden/reusable-ci/v3/internal/livetest"
func TestCase(t *T) { @CLIIn(t, target, repo, opts, "report") }`, "report", false},
		{"raw command", imported + "func TestCase(t *T) { @livetest.CLI(t, target, repo, `validate`) }", "validate", false},
		{"constant", imported + `const group = "version"
func TestCase(t *T) { @livetest.CLI(t, target, repo, group) }`, "version", false},
		{"constant concatenation", imported + `const group = "ver" + "sion"
func TestCase(t *T) { @livetest.CLI(t, target, repo, group) }`, "version", false},
		{"slice", imported + `func TestCase(t *T) { @livetest.CLI(t, target, repo, []string{"artifact", "version"}...) }`, "artifact", false},
		{"local argv", imported + `func TestCase(t *T) { args := []string{"release"}; @livetest.CLI(t, target, repo, args...) }`, "release", false},
		{"tail append", imported + `func TestCase(t *T) { args := append([]string{"container"}, dynamic...); args = append(args, "version"); @livetest.CLI(t, target, repo, args...) }`, "container", false},
		{"empty then append in closure", imported + `func TestCase(t *T) { publish := func() { args := make([]string, 0, 12); args = append(args, "release"); for _, asset := range assets { args = append(args, asset) }; @livetest.CLIIn(t, target, repo, opts, args...) }; publish() }`, "release", false},
		{"argv helper", imported + `func argv(t *T) []string { return []string{"artifact", "upload"} }
func TestCase(t *T) { @livetest.CLI(t, target, repo, argv(t)...) }`, "artifact", false},
		{"argv helper parameter", imported + `func argv(group string) []string { return []string{group} }
func TestCase(t *T) { @livetest.CLI(t, target, repo, argv("release")...) }`, "release", false},
		{"forward labels are not argv", imported + `func forward(t *T, label string, args ...string) { @livetest.CLIIn(t, target, repo, opts, args...) }
func TestCase(t *T) { forward(t, "version", "container", "ledger") }`, "container", false},
		{"method forwarding", imported + `type fixture struct{}
func newFixture() fixture { return fixture{} }
func (f fixture) run(t *T, args ...string) { @livetest.CLIIn(t, target, repo, opts, args...) }
func (f fixture) mustRun(t *T, label string, args ...string) { f.run(t, args...) }
func TestCase(t *T) { f := newFixture(); f.mustRun(t, "version", "container") }`, "container", false},
		{"subtest callback", imported + `func TestCase(t *T) { t.Run("version", func(t *T) { @livetest.CLI(t, target, repo, "doctor") }) }`, "doctor", false},
		{"later assignment cannot backfill", imported + `func TestCase(t *T) { args := dynamic; @livetest.CLI(t, target, repo, args...); args = []string{"version"} }`, "", true},
		{"earlier assignment replaced", imported + `func TestCase(t *T) { args := []string{"version"}; args = []string{"release"}; @livetest.CLI(t, target, repo, args...) }`, "release", false},
		{"tuple argv overwrite", imported + `func replacement() ([]string, error) { return []string{"release"}, nil }
func TestCase(t *T) { args := []string{"version"}; args, err := replacement(); _ = err; @livetest.CLI(t, target, repo, args...) }`, "", true},
		{"range key and value overwrite", imported + `func TestCase(t *T) { group := "version"; args := []string{"version"}; for group, args = range rows { @livetest.CLI(t, target, repo, group); @livetest.CLI(t, target, repo, args...) } }`, "", true},
		{"compound argv overwrite", imported + `func TestCase(t *T) { group := "not-"; group += "version"; @livetest.CLI(t, target, repo, group) }`, "", true},
		{"tuple CLI alias overwrite", imported + `func TestCase(t *T) { invoke := livetest.CLI; invoke, err := replacement(); _ = err; invoke(t, target, repo, "version") }`, "", false},
		{"unknown first arg", imported + `func TestCase(t *T) { @livetest.CLI(t, target, repo, dynamic, "version") }`, "", true},
		{"unknown prefix append", imported + `func TestCase(t *T) { @livetest.CLI(t, target, repo, append(dynamic, "version")...) }`, "", true},
		{"nonempty make", imported + `func TestCase(t *T) { args := make([]string, 1); args = append(args, "version"); @livetest.CLI(t, target, repo, args...) }`, "", true},
		{"indexed overwrite", imported + `func TestCase(t *T) { args := []string{"version"}; args[0] = dynamic; @livetest.CLI(t, target, repo, args...) }`, "", true},
		{"conditional replacement", imported + `func TestCase(t *T) { args := []string{"version"}; if condition { args = []string{"release"} }; @livetest.CLI(t, target, repo, args...) }`, "", true},
		{"conditional initial append", imported + `func TestCase(t *T) { args := []string{}; if condition { args = append(args, "version") }; @livetest.CLI(t, target, repo, args...) }`, "", true},
		{"function field is unresolved", imported + `func TestCase(t *T) { @livetest.CLI(t, target, repo, row.args(t)...) }`, "", true},
		{"recursive argv helper", imported + `func argv() []string { return argv() }
func TestCase(t *T) { @livetest.CLI(t, target, repo, argv()...) }`, "", true},
		{"JSON and comments", imported + "// livetest.CLI(t, target, repo, \"version\")\nfunc TestCase(t *T) { body := `{\"version\":1}`; _ = body }", "", false},
		{"helper name and labels", imported + `func version(label string) {}
func TestCase(t *T) { version("version") }`, "", false},
		{"uncalled helper", imported + `func helper(t *T) { livetest.CLI(t, target, repo, "version") }
func TestCase(t *T) {}`, "", false},
		{"uncalled closure", imported + `func TestCase(t *T) { helper := func() { livetest.CLI(t, target, repo, "version") }; _ = helper }`, "", false},
		{"inert callback", imported + `func ignore(callback func()) {}
func TestCase(t *T) { ignore(func() { livetest.CLI(t, target, repo, "version") }) }`, "", false},
		{"shadowed Run callback", imported + `func TestCase(t *T) { { t := fake; t.Run("version", func() { livetest.CLI(t, target, repo, "version") }) } }`, "", false},
		{"not a Go test name", imported + `func Testcase(t *T) { livetest.CLI(t, target, repo, "version") }`, "", false},
		{"not a Go test signature", imported + `func TestCase(t *Other) { livetest.CLI(t, target, repo, "version") }`, "", false},
		{"wrong import", `import livetest "example.org/livetest"
func TestCase(t *T) { livetest.CLI(t, target, repo, "version") }`, "", false},
		{"unimported name", `func TestCase(t *T) { livetest.CLI(t, target, repo, "version") }`, "", false},
		{"parameter shadows import", imported + `func shadow(livetest Fake) { livetest.CLI(t, target, repo, "version") }
func TestCase(t *T) { shadow(fake) }`, "", false},
		{"local shadows import", imported + `func TestCase(t *T) { livetest := fake; livetest.CLI(t, target, repo, "version") }`, "", false},
		{"dot shadow", `import . "github.com/diggsweden/reusable-ci/v3/internal/livetest"
func TestCase(t *T) { CLI := fake; CLI(t, target, repo, "version") }`, "", false},
		{"helper shadow", imported + `func argv() []string { return []string{"version"} }
func TestCase(t *T) { argv := external; @livetest.CLI(t, target, repo, argv()...) }`, "", true},
		{"append shadow", imported + `func TestCase(t *T) { append := external; @livetest.CLI(t, target, repo, append([]string{}, "version")...) }`, "", true},
		{"helper with multiple returns", imported + `func argv() []string { if condition { return []string{"version"} }; return dynamic }
func TestCase(t *T) { @livetest.CLI(t, target, repo, argv()...) }`, "", true},
		{"unrelated same method name", imported + `type fixture struct{}
func (f fixture) run(t *T, args ...string) { livetest.CLI(t, target, repo, args...) }
func TestCase(t *T) { f := foreign(); f.run(t, "version") }`, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "package fixture\nimport \"testing\"\n" + strings.ReplaceAll(tc.source, "*T", "*testing.T") + "\n"
			parts := strings.Split(source, "@")
			source = strings.Join(parts, "")
			fset := token.NewFileSet()

			file, err := parser.ParseFile(fset, "scenario_test.go", source, 0)
			if err != nil {
				t.Fatal(err)
			}

			got, unknown := liveInvocations(fset, []*ast.File{file})
			want := map[string][]string{}

			var wantUnknown []string

			if tc.group != "" || tc.unresolved {
				if len(parts) < 2 {
					t.Fatal("fixture has no diagnostic marker")
				}

				locations := make([]string, 0, len(parts)-1)

				offset := 1
				for _, part := range parts[:len(parts)-1] {
					offset += len(part)
					locations = append(locations, fset.Position(token.Pos(offset)).String())
				}

				slices.Sort(locations)

				if tc.group != "" {
					want[tc.group] = locations
				} else {
					wantUnknown = locations
				}
			}

			if !reflect.DeepEqual(got, want) || !slices.Equal(unknown, wantUnknown) {
				t.Errorf("live invocation grading = %v, unresolved %v; want %v, unresolved %v", got, unknown, want, wantUnknown)
			}
		})
	}
}

func TestLiveParityCrossFileFixtures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, body string
		want       map[string][]string
	}{
		{"called helper", `forward(t, "version", "container")`, map[string][]string{"container": {"helper_test.go:3:53"}}},
		{"uncalled helper", `_ = "version"`, map[string][]string{}},
		{"local helper shadow", `forward := fake; forward(t, "version")`, map[string][]string{}},
		{"per-file import identity", `live.CLI(t, target, repo, "version")`, map[string][]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			files := make([]*ast.File, 0, 2)

			for _, input := range []struct{ name, source string }{
				{"helper_test.go", `package fixture
import live "github.com/diggsweden/reusable-ci/v3/internal/livetest"
func forward(t any, label string, args ...string) { live.CLIIn(t, target, repo, opts, args...) }
func unused() []string { return []string{"version"} }
`},
				{"scenario_test.go", `package fixture
import "testing"
import live "example.org/livetest"
func TestCase(t *testing.T) { ` + tc.body + ` }
`},
			} {
				file, err := parser.ParseFile(fset, input.name, input.source, 0)
				if err != nil {
					t.Fatal(err)
				}

				files = append(files, file)
			}

			got, unknown := liveInvocations(fset, files)
			if !reflect.DeepEqual(got, tc.want) || len(unknown) != 0 {
				t.Errorf("cross-file invocation grading = %v, unresolved %v; want %v", got, unknown, tc.want)
			}
		})
	}
}

func TestLiveParityDependencyFixtures(t *testing.T) {
	t.Parallel()

	const (
		provider   = internalPrefix + "domain/provider"
		runcontext = internalPrefix + "runcontext"
	)
	for _, tc := range []struct {
		name      string
		files     map[string]string
		want      []string
		wantError bool
	}{
		{"direct alias", map[string]string{"internal/command/main.go": "package p\nimport p \"" + provider + "\"\n"}, []string{"internal/command/main.go:2:8 imports " + provider}, false},
		{"raw dot import", map[string]string{"internal/command/main.go": "package p\nimport . `" + runcontext + "`\n"}, []string{"internal/command/main.go:2:8 imports " + runcontext}, false},
		{"inert text and test only", map[string]string{"internal/command/main.go": "package p\n// import \"" + provider + "\"\nvar text = `\"" + runcontext + "\"`\n", "internal/command/main_test.go": "package p\nimport _ \"" + provider + "\"\n"}, nil, false},
		{"recursive and cyclic", map[string]string{
			"internal/command/main.go": "package p\nimport _ \"" + internalPrefix + "helper\"\n",
			"internal/helper/main.go":  "package p\nimport _ \"" + internalPrefix + "command\"\nimport _ \"" + internalPrefix + "leaf\"\n",
			"internal/leaf/main.go":    "package p\nimport _ \"" + provider + "\"\n",
		}, []string{"internal/command/main.go:2:8 imports " + internalPrefix + "helper", "internal/helper/main.go:3:8 imports " + internalPrefix + "leaf", "internal/leaf/main.go:2:8 imports " + provider}, false},
		{"external boundary", map[string]string{"internal/command/main.go": "package p\nimport _ \"example.org/internal/runcontext\"\n"}, nil, false},
		{"tagged production file", map[string]string{"internal/command/other.go": "//go:build other\n\npackage p\nimport _ \"" + provider + "\"\n"}, []string{"internal/command/other.go:4:8 imports " + provider}, false},
		{"missing internal dependency", map[string]string{"internal/command/main.go": "package p\nimport _ \"" + internalPrefix + "absent\"\n"}, nil, true},
		{"parse failure", map[string]string{"internal/command/main.go": "not go"}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tree := fstest.MapFS{}
			for name, body := range tc.files {
				tree[name] = &fstest.MapFile{Data: []byte(body)}
			}

			got, err := liveForgeDependency(tree, "internal/command")
			if (err != nil) != tc.wantError || !reflect.DeepEqual(got, tc.want) {
				t.Errorf("forge dependency grading = %v, err %v; want %v, error %v", got, err, tc.want, tc.wantError)
			}
		})
	}
}
