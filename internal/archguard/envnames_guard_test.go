// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archguard

import (
	"bytes"
	"errors"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"mvdan.cc/sh/v3/syntax"
)

// TestNoBPOEnvNamespaceOutsideShim keeps the removed BPO_-prefixed env
// namespace out of the tree. It was private to one command and drifted
// from the container group's shared env names; the canonical names
// replaced it and the namespace was deleted — nothing may read or set a
// BPO_ var again. Only source and workflow files are scanned.
func TestNoBPOEnvNamespaceOutsideShim(t *testing.T) {
	t.Parallel()

	// Only this guard itself may mention the dead namespace.
	bpoAllowedFiles := map[string]bool{
		"internal/archguard/envnames_guard_test.go": true,
	}

	root := reporoot.Path(t)

	var offenders []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		name := entry.Name()
		if entry.IsDir() {
			if name == ".git" || name == "dist" || name == "node_modules" {
				return filepath.SkipDir
			}

			return nil
		}

		switch filepath.Ext(name) {
		case ".go", ".yml", ".yaml", ".sh":
		default:
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		if bpoAllowedFiles[filepath.ToSlash(rel)] {
			return nil
		}

		content, readErr := os.ReadFile(path) //nolint:gosec // test walks repo-local files.
		if readErr != nil {
			return readErr
		}

		found, scanErr := bpoSourceReference(path, content)
		if scanErr != nil {
			return scanErr
		}

		if found {
			offenders = append(offenders, filepath.ToSlash(rel))
		}

		return nil
	})
	require.NoError(t, err)

	require.Emptyf(t, offenders,
		"deprecated BPO_ env namespace referenced; use the canonical shared environment names")
}

// bpoSourceReference ignores Go/YAML comments and evaluates file-local Go
// string constants. It conservatively checks string data, not just env calls:
// generated scripts and flag sources must not reintroduce the namespace either.
// Imported/cross-file constants and dynamic names are not resolved here.
//
// Shell files are parsed too, so a name in a `# comment` is not a reference
// while one in a parameter expansion or an assignment is. Shell EMBEDDED in
// string data — a script this repository generates and hands to a runner —
// still receives the lexical check, and deliberately: that text is not shell
// here, it is a value, and a name in it will become a reference somewhere this
// guard cannot see. Over-reporting is the right direction there.
func bpoSourceReference(path string, content []byte) (bool, error) {
	switch filepath.Ext(path) {
	case ".go":
		return bpoGoReference(path, content)
	case ".yml", ".yaml":
		decoder := yaml.NewDecoder(strings.NewReader(string(content)))
		found := false

		for {
			var document yaml.Node
			if err := decoder.Decode(&document); err != nil {
				if errors.Is(err, io.EOF) {
					return found, nil
				}

				return false, err
			}

			nodes := []*yaml.Node{&document}
			for len(nodes) > 0 {
				node := nodes[len(nodes)-1]
				nodes = nodes[:len(nodes)-1]

				if node.Kind == yaml.ScalarNode && strings.Contains(node.Value, "BPO_") {
					found = true
				}
				// Anchored values are already in Content. Following Alias as well
				// would duplicate them and could cycle on a recursive YAML alias.
				nodes = append(nodes, node.Content...)
			}
		}
	case ".sh":
		return bpoShellReference(path, content)
	default:
		return strings.Contains(string(content), "BPO_"), nil
	}
}

// bpoShellReference reports a namespaced name that a shell script actually
// uses, rather than one that appears in it.
//
// The parse is what makes the distinction, and it makes it structurally rather
// than by a rule written here: a comment's text is a *syntax.Comment, and a
// comment is never a word, so it is not one of the node kinds below. A name in
// a parameter expansion, an assignment, or an `export`/`unset` argument reaches
// the walk as a *syntax.Lit — including the Lit INSIDE a ParamExp, which is why
// there is no separate case for one.
//
// Single-quoted text is reported even though the shell would not expand it.
// That is deliberate and is the one place this over-reports: 'BPO_TOKEN' is
// almost always a name being passed somewhere that will expand it, and the
// namespace is retired, so the name should not be in a script at all.
//
// A script that will not parse is reported, not skipped. Declining to answer
// for a file is the fail-open the parse was meant to remove.
func bpoShellReference(path string, content []byte) (bool, error) {
	file, err := syntax.NewParser(syntax.KeepComments(true), syntax.Variant(syntax.LangBash)).
		Parse(bytes.NewReader(content), path)
	if err != nil {
		return true, nil //nolint:nilerr // an unparsable script is reported, not excused.
	}

	found := false

	syntax.Walk(file, func(node syntax.Node) bool {
		switch node := node.(type) {
		case *syntax.Lit:
			if strings.Contains(node.Value, "BPO_") {
				found = true
			}
		case *syntax.SglQuoted:
			if strings.Contains(node.Value, "BPO_") {
				found = true
			}
		}

		return true
	})

	return found, nil
}

func bpoGoReference(path string, content []byte) (bool, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, path, content, 0)
	if err != nil {
		return false, err
	}

	info := &types.Info{Types: make(map[ast.Expr]types.TypeAndValue)}
	// A source-file scan cannot type-check imports or sibling declarations.
	// Keep the independently resolved constants after those errors, and also
	// scan literals so incomplete type information cannot hide literal names.
	config := types.Config{Error: func(error) {}}
	_, _ = config.Check(file.Name.Name, fset, []*ast.File{file}, info)
	found := false

	ast.Inspect(file, func(node ast.Node) bool {
		if lit, ok := node.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			value, err := strconv.Unquote(lit.Value)
			if err == nil && strings.Contains(value, "BPO_") {
				found = true
			}
		}

		if expr, ok := node.(ast.Expr); ok {
			if value := info.Types[expr].Value; value != nil && value.Kind() == constant.String && strings.Contains(constant.StringVal(value), "BPO_") {
				found = true
			}
		}

		return !found
	})

	return found, nil
}

func TestBPOSourceReference(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, ext, source string
		want              bool
	}{
		{"go comments", ".go", "package p // BPO_TOKEN\n/* BPO_TOKEN */", false},
		{"go identifier", ".go", `package p; const BPO_TOKEN = "CANONICAL_TOKEN"`, false},
		{"go literal", ".go", `package p; var name = "BPO_TOKEN"`, true},
		{"go escaped literal", ".go", `package p; var name = "\x42PO_TOKEN"`, true},
		{"go constant concatenation", ".go", `package p; const prefix = "B" + "P"; const name = prefix + "O_TOKEN"; var _ = name`, true},
		{"go local constant", ".go", `package p; func f() string { const prefix = "BP"; return prefix + "O_TOKEN" }`, true},
		{"go typed constant", ".go", `package p; type key string; const prefix key = "BP"; const name = string(prefix) + "O_TOKEN"`, true},
		{"go shadowed constant", ".go", `package p; const prefix = "BP"; func f() string { const prefix = "CI"; return prefix + "O_TOKEN" }`, false},
		{"go unresolved import literal", ".go", `package p; import env "os"; var _ = env.Getenv("BPO_TOKEN")`, true},
		{"go unresolved import constant", ".go", `package p; import env "os"; const prefix = "BP"; var _ = env.Getenv(prefix + "O_TOKEN")`, true},
		{"go generated comment is data", ".go", "package p; var script = `# BPO_TOKEN`", true},
		{"go near miss", ".go", `package p; const name = "BP" + "OTOKEN"`, false},
		{"yaml comments", ".yml", "# BPO_TOKEN\nenv: {} # BPO_TOKEN\n", false},
		{"yaml key", ".yaml", "env:\n  BPO_TOKEN: value\n", true},
		{"yaml expression", ".yml", "value: ${{ env.BPO_TOKEN }}\n", true},
		{"yaml escaped scalar", ".yml", "env: {\"\\x42PO_TOKEN\": value}\n", true},
		{"yaml later document", ".yaml", "env: {}\n---\nenv: {BPO_TOKEN: value}\n", true},
		{"yaml alias", ".yml", "env: &vars {BPO_TOKEN: value}\ncopy: *vars\n", true},
		{"yaml recursive alias", ".yml", "env: &vars {self: *vars}\n", false},
		{"yaml block scalar is data", ".yml", "run: |\n  # BPO_TOKEN\n", true},
		{"yaml quoted hash is data", ".yml", "value: '# BPO_TOKEN'\n", true},
		// Shell used to be lexical, so a name in a comment counted. It is
		// parsed now, which is what D200 asked for: a reference is a use.
		{"shell comment is not a reference", ".sh", "# BPO_TOKEN\n", false},
		{"shell trailing comment is not a reference", ".sh", "echo hello # BPO_TOKEN\n", false},
		{"shell parameter expansion", ".sh", "printf '%s' \"$BPO_TOKEN\"\n", true},
		{"shell braced expansion", ".sh", "printf '%s' \"${BPO_TOKEN}\"\n", true},
		{"shell expansion with a default", ".sh", "printf '%s' \"${BPO_TOKEN:-none}\"\n", true},
		{"shell assignment", ".sh", "BPO_TOKEN=value\n", true},
		{"shell export", ".sh", "export BPO_TOKEN\n", true},
		{"shell unset", ".sh", "unset BPO_TOKEN\n", true},
		{"shell single-quoted mention is still reported", ".sh", "printf '%s' 'BPO_TOKEN'\n", true},
		{"shell unrelated name", ".sh", "printf '%s' \"$OTHER_TOKEN\"\n", false},
		{"unparsable shell is reported, not skipped", ".sh", "if [ -n \"$x\" ]; then\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			found, err := bpoSourceReference("fixture"+tc.ext, []byte(tc.source))
			require.NoError(t, err)
			require.Equal(t, tc.want, found)
		})
	}
}

func TestBPOSourceReferenceRejectsMalformedSource(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ path, source string }{
		{"fixture.go", `package p; const name = "BPO_TOKEN`},
		{"fixture.yml", "env: [\n"},
		{"fixture.yaml", "env: {BPO_TOKEN: value}\n---\nenv: [\n"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			_, err := bpoSourceReference(tc.path, []byte(tc.source))
			require.Error(t, err)
		})
	}
}
