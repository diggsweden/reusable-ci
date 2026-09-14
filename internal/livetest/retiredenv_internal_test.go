// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestLiveConsumer_NoRetiredAmbientContractReads keeps the retired ambient
// inputs from coming back. The lab's contract is a signed file the producer
// hands over; these variables were the loose environment it replaced, and a
// consumer reading one again would take unverified input from whoever set it.
//
// Discover Go, shell and embedded shell templates, excluding test fixtures.
// This is a source tripwire, not data-flow analysis: Go literal concatenations
// are folded, but names assembled through identifiers or shell indirection are
// outside its scope. Go comments and whole-line shell comments are ignored.
func TestLiveConsumer_NoRetiredAmbientContractReads(t *testing.T) {
	t.Parallel()

	for _, path := range liveConsumerSources(t, ".", filepath.Join("..", "..", "scripts", "ci")) {
		body, err := os.ReadFile(path) //nolint:gosec // test walks repo-local sources.
		if err != nil {
			t.Fatal(err)
		}

		if mentionsRetiredAmbientInput(t, path, body) {
			t.Errorf("%s still contains a retired ambient input", path)
		}
	}
}

func mentionsRetiredAmbientInput(t *testing.T, path string, body []byte) bool {
	t.Helper()

	pattern := regexp.MustCompile(`\bLAB_(?:TARGETS|CA_FILE|FULCIO_URL|FULCIO_ISSUERS)\b`)

	if !strings.HasSuffix(path, ".go") {
		for _, line := range strings.Split(string(body), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "#") && pattern.MatchString(line) {
				return true
			}
		}

		return false
	}

	file, err := parser.ParseFile(token.NewFileSet(), path, body, 0)
	if err != nil {
		t.Fatal(err)
	}

	return retiredEnvironmentRead(file, pattern)
}

// retiredEnvironmentRead reports a retired name that is actually READ from the
// environment, rather than any string that happens to contain it.
//
// The distinction matters in the direction that erodes guards. A diagnostic
// telling an operator "LAB_TARGETS was retired; pass the signed contract
// instead" is exactly the message this rule wants people to write, and the old
// check flagged it — so the way to keep the build green was to stop saying the
// name, which makes the codebase worse and the guard no more effective. What is
// forbidden is reading the variable, not naming it.
//
// The recognised shapes are an argument to an environment lookup, directly or
// through a constant or single-assignment local bound to the literal. Names
// assembled at run time remain out of scope, as they were.
func retiredEnvironmentRead(file *ast.File, pattern *regexp.Regexp) bool {
	literals := namesBoundToLiterals(file)
	found := false

	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !environmentLookup(call.Fun) {
			return !found
		}

		for _, arg := range call.Args {
			for _, value := range literalStrings(arg, literals) {
				if pattern.MatchString(value) {
					found = true
				}
			}
		}

		return !found
	})

	return found
}

// namesBoundToLiterals collects every string literal each constant or local is
// assigned, so `const key = "LAB_CA_FILE"` followed by a lookup of key, or
// `"LAB_" + suffix` with a constant suffix, is recognised. A name assigned more
// than once keeps all its values: the tripwire over-reports rather than miss a
// read that follows the retired assignment.
func namesBoundToLiterals(file *ast.File) map[string][]string {
	bound := map[string][]string{}

	record := func(name ast.Expr, value ast.Expr) {
		ident, ok := name.(*ast.Ident)
		if !ok {
			return
		}

		bound[ident.Name] = append(bound[ident.Name], literalStrings(value, bound)...)
	}

	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.ValueSpec:
			for i, name := range node.Names {
				if i < len(node.Values) {
					record(name, node.Values[i])
				}
			}
		case *ast.AssignStmt:
			if len(node.Lhs) == len(node.Rhs) {
				for i, lhs := range node.Lhs {
					record(lhs, node.Rhs[i])
				}
			}
		}

		return true
	})

	return bound
}

// environmentLookup names the calls that read a variable out of the process
// environment. A lookup passed as a function value — the injected-getenv seam
// this repository uses — is covered by the bare-identifier case.
func environmentLookup(fun ast.Expr) bool {
	switch fun := ast.Unparen(fun).(type) {
	case *ast.SelectorExpr:
		pkg, ok := fun.X.(*ast.Ident)

		return ok && (pkg.Name == "os" || pkg.Name == "syscall") &&
			(fun.Sel.Name == "Getenv" || fun.Sel.Name == "LookupEnv")
	case *ast.Ident:
		// An injected lookup: env("NAME"), getenv("NAME").
		lowered := strings.ToLower(fun.Name)

		return strings.Contains(lowered, "env") || strings.Contains(lowered, "lookup")
	default:
		return false
	}
}

// literalStrings folds a string literal, a parenthesised or concatenated one,
// and identifiers bound to such values, into every text it can take.
func literalStrings(expression ast.Expr, bound map[string][]string) []string {
	switch node := expression.(type) {
	case *ast.BasicLit:
		if node.Kind == token.STRING {
			if value, err := strconv.Unquote(node.Value); err == nil {
				return []string{value}
			}
		}
	case *ast.Ident:
		return bound[node.Name]
	case *ast.ParenExpr:
		return literalStrings(node.X, bound)
	case *ast.BinaryExpr:
		if node.Op == token.ADD {
			var joined []string

			for _, left := range literalStrings(node.X, bound) {
				for _, right := range literalStrings(node.Y, bound) {
					joined = append(joined, left+right)
				}
			}

			return joined
		}
	}

	return nil
}

func TestRetiredAmbientInput_SourceForms(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, path, body string
		want             bool
	}{
		{name: "literal", path: "consumer.go", body: `package p; var x = os.Getenv("LAB_TARGETS")`, want: true},
		{name: "concatenated", path: "consumer.go", body: `package p; var x = os.Getenv("LAB_" + ("TARGETS"))`, want: true},
		{name: "named_literal", path: "consumer.go", body: `package p; const key = "LAB_CA_FILE"; var x = os.LookupEnv(key)`, want: true},
		{name: "assigned_local", path: "consumer.go", body: `package p; func f() { key := "LAB_CA_FILE"; _ = os.Getenv(key) }`, want: true},
		{name: "injected_lookup", path: "consumer.go", body: `package p; func f(env func(string) string) { _ = env("LAB_TARGETS") }`, want: true},
		{name: "syscall_getenv", path: "consumer.go", body: `package p; func f() { _, _ = syscall.Getenv("LAB_FULCIO_URL") }`, want: true},
		{name: "constant_suffix", path: "consumer.go", body: `package p; const suffix = "TARGETS"; var x = os.Getenv("LAB_" + suffix)`, want: true},
		{name: "chained_constants", path: "consumer.go", body: `package p; const prefix = "LAB_"; const key = prefix + "CA_FILE"; var x = os.Getenv(key)`, want: true},
		{name: "retired_after_a_safe_value", path: "consumer.go", body: `package p; func f() { key := "SAFE"; _ = os.Getenv(key); key = "LAB_CA_FILE"; _ = os.Getenv(key) }`, want: true},
		{name: "reassigned_away_over_reports", path: "consumer.go", body: `package p; func f() { key := "LAB_CA_FILE"; key = "SAFE"; _ = os.Getenv(key) }`, want: true},
		{name: "safe_values_only", path: "consumer.go", body: `package p; func f() { key := "SAFE"; key = "ALSO_SAFE"; _ = os.Getenv(key) }`},

		// Naming a retired variable is not reading one. The old check flagged
		// these, which made the way to a green build "stop saying the name" —
		// worse documentation and no more safety.
		{name: "diagnostic_message", path: "consumer.go", body: `package p; var msg = "LAB_TARGETS was retired; pass the signed contract instead"`},
		{name: "doc_constant", path: "consumer.go", body: `package p; const help = "set LAB_CA_FILE" // retired, documented for the migration note`},
		{name: "name_in_an_unrelated_call", path: "consumer.go", body: `package p; func f() { log.Printf("do not set %s", "LAB_FULCIO_ISSUERS") }`},
		{name: "constant_bound_but_never_read", path: "consumer.go", body: `package p; const key = "LAB_CA_FILE"; var _ = key`},
		{name: "go_comment", path: "consumer.go", body: "package p\n// os.Getenv(\"LAB_TARGETS\")"},
		{name: "supported_name", path: "consumer.go", body: `package p; var x = os.Getenv("LAB_TARGETS_FILE")`},
		{name: "template", path: "probe.sh.tmpl", body: `printf '%s' "${LAB_FULCIO_URL}"`, want: true},
		{name: "shell", path: "probe.sh", body: `printf '%s' "$LAB_FULCIO_ISSUERS"`, want: true},
		{name: "shell_comment", path: "probe.sh.tmpl", body: "  # $LAB_TARGETS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := mentionsRetiredAmbientInput(t, tc.path, []byte(tc.body)); got != tc.want {
				t.Fatalf("reported=%t, want %t", got, tc.want)
			}
		})
	}
}

func TestLiveConsumerSources_IncludesTemplatesAndExcludesFixtures(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, dir := range []string{"probes", "testdata"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	want := []string{"consumer.go", "probes/probe.sh.tmpl", "script.sh"}
	for _, name := range append(slices.Clone(want), "consumer_test.go", "testdata/fixture.sh.tmpl") {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got := liveConsumerSources(t, root)
	for i := range want {
		want[i] = filepath.Join(root, want[i])
	}

	if !slices.Equal(got, want) {
		t.Fatalf("sources=%v, want %v", got, want)
	}
}

// liveConsumerSources lists everything that could read the lab contract: this
// package's own non-test sources, and the CI scripts that invoke the suite.
func liveConsumerSources(t *testing.T, dirs ...string) []string {
	t.Helper()

	var paths []string

	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if entry.IsDir() {
				// testdata holds contract fixtures, which name the retired
				// variables on purpose to prove they are rejected.
				if path != dir && entry.Name() == "testdata" {
					return filepath.SkipDir
				}

				return nil
			}

			name := entry.Name()
			switch {
			case strings.HasSuffix(name, "_test.go"):
			case strings.HasSuffix(name, ".go"), strings.HasSuffix(name, ".sh"), strings.HasSuffix(name, ".sh.tmpl"):
				paths = append(paths, path)
			}

			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}

	if len(paths) == 0 {
		t.Fatal("found no live-consumer sources to scan")
	}

	return paths
}

// TestLiveConsumerInventory_EveryProbeIsEmbeddedAndScanned reconciles the
// scanned set against what actually reaches a runner.
//
// The scan walks directories, which answers "what is on disk" rather than "what
// executes". Those differ in both directions and only one is harmless. A
// template on disk that nothing embeds is dead weight — the scan reads it, and
// the guard is stricter than it needs to be. A file that IS embedded and does
// not match the walk's suffixes would be shipped to a runner unscanned, which
// is the failure this guard exists to prevent.
//
// Six probes are embedded today, each by an explicit //go:embed line, so the
// two sets can simply be compared.
func TestLiveConsumerInventory_EveryProbeIsEmbeddedAndScanned(t *testing.T) {
	t.Parallel()

	embedded := embeddedProbePaths(t)
	if len(embedded) < 3 {
		t.Fatalf("found %d //go:embed probe directives; the regexp, not the source, is what was measured", len(embedded))
	}

	onDisk := map[string]bool{}

	entries, err := os.ReadDir("probes")
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			onDisk["probes/"+entry.Name()] = true
		}
	}

	scanned := map[string]bool{}
	for _, path := range liveConsumerSources(t, ".") {
		scanned[filepath.ToSlash(strings.TrimPrefix(path, "./"))] = true
	}

	for _, path := range embedded {
		if !onDisk[path] {
			t.Errorf("%s is embedded but not present; the build would fail, so this is a stale directive", path)
		}

		if !scanned[path] {
			t.Errorf("%s is embedded into a runner probe but the retired-input scan does not read it; "+
				"a retired variable there would ship unnoticed", path)
		}
	}

	for path := range onDisk {
		if !slices.Contains(embedded, path) {
			t.Errorf("%s sits in probes/ but nothing embeds it; delete it or embed it, so the inventory means something", path)
		}
	}
}

// embedDirective matches a //go:embed line and captures its patterns.
var embedDirective = regexp.MustCompile(`(?m)^//go:embed\s+(.+?)\s*$`)

// embeddedProbePaths lists every file embedded by any non-test Go source in
// dir. It used to read only inrunnerprobes.go, so a directive added to another
// file, or naming a file outside probes/, escaped the inventory.
func embeddedProbePaths(t *testing.T) []string {
	t.Helper()

	paths, err := embeddedPaths(".")
	if err != nil {
		t.Fatal(err)
	}

	return paths
}

func embeddedPaths(dir string) ([]string, error) {
	sources, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}

	var paths []string

	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}

		body, err := os.ReadFile(source) //nolint:gosec // test reads package-local sources.
		if err != nil {
			return nil, err
		}

		for _, match := range embedDirective.FindAllStringSubmatch(string(body), -1) {
			paths = append(paths, strings.Fields(match[1])...)
		}
	}

	slices.Sort(paths)

	return paths, nil
}

// TestEmbeddedPaths_FindsDirectivesInEveryPackageSource puts directives in a
// second source file, with two patterns on one line and a path outside
// probes/, beside a test file whose directive must be ignored.
func TestEmbeddedPaths_FindsDirectivesInEveryPackageSource(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for name, body := range map[string]string{
		"inrunnerprobes.go": "package p\n\n//go:embed probes/a.sh\nvar a string\n",
		"extra.go":          "package p\n\n//go:embed probes/b.sh.tmpl templates/c.yaml\nvar b embed.FS\n",
		"extra_test.go":     "package p\n\n//go:embed probes/ignored.sh\nvar c string\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := embeddedPaths(dir)
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"probes/a.sh", "probes/b.sh.tmpl", "templates/c.yaml"}; !slices.Equal(got, want) {
		t.Fatalf("embedded = %v, want %v", got, want)
	}
}
