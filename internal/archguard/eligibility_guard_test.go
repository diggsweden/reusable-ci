// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archguard

import (
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
)

// Every guard in this package selects its input the same way: a `.go` file that
// is not a `_test.go` file. What that filter actually admits depends on how the
// guard then reads the file, and the two mechanisms in use disagree.
//
// The source-walking guards (layering, credential, exec-wrap, provider-switch)
// parse with go/parser, which ignores build constraints. They read a
// `//go:build !linux` file on Linux, including code that never compiles here.
// The environment guard loads through the compiler's export metadata, which
// honours build constraints, so the same file is invisible to it: no
// violations, no diagnostic, a clean pass over nothing.
//
// Neither behaviour is wrong on its own, and neither is announced. The failure
// mode is a guard that quietly stops covering part of the tree. So the
// constrained files are declared here, one row each, and anything new fails
// until someone says how the scanners should treat it.
//
// buildConstrainedProductFiles is that declaration. `_other`/`!linux` variants
// are the ones the environment guard cannot see on a Linux CI runner, which is
// why the eligibility check below holds them to a source-level rule instead.
func buildConstrainedProductFiles() map[string]string {
	return map[string]string{
		"internal/cliio/lock_unix.go":          "unix",
		"internal/safeexec/hardening_linux.go": "linux",
		"internal/safeexec/hardening_other.go": "!linux",
		"internal/safeexec/swap_linux.go":      "linux",
		"internal/safeexec/swap_other.go":      "!linux",
	}
}

// envReadingSelectors are the direct environment reads the environment guard
// exists to police. A build-constrained file the guard cannot load must not
// contain one, because nothing would report it.
func envReadingSelectors() []string {
	return []string{"Getenv", "LookupEnv", "Environ", "Setenv", "Unsetenv"}
}

func TestGuardEligibility_ConstrainedAndGeneratedProductFilesAreDeclared(t *testing.T) {
	t.Parallel()

	declared := buildConstrainedProductFiles()
	root := reporoot.Path(t)

	var (
		found     = map[string]string{}
		generated []string
		scanned   int
	)

	for _, tree := range []string{"internal", "cmd"} {
		walkProductGoFiles(t, filepath.Join(root, tree), root, func(rel, source string) {
			scanned++

			if tag, ok := buildConstraintOf(t, rel, source); ok {
				found[rel] = tag
			}

			if hasGeneratedMarker(source) {
				generated = append(generated, rel)
			}
		})
	}

	if scanned < 200 {
		t.Fatalf("scanned %d product files; the walk, not the tree, is what was measured", scanned)
	}

	for rel, tag := range found {
		want, ok := declared[rel]
		switch {
		case !ok:
			t.Errorf("undeclared build-constrained product file %s (//go:build %s)\n"+
				"Add it to buildConstrainedProductFiles and decide what each scanner does with it. The "+
				"source-walking guards will parse it whatever the current build configuration is; the "+
				"environment guard will not load it at all unless the constraint is satisfied.", rel, tag)
		case want != tag:
			t.Errorf("%s now says //go:build %s, declared as %s; re-check which scanners still reach it", rel, tag, want)
		}
	}

	for rel := range declared {
		if _, ok := found[rel]; !ok {
			t.Errorf("declared build-constrained file %s no longer carries a constraint (or no longer exists); drop the row", rel)
		}
	}

	sort.Strings(generated)

	if len(generated) > 0 {
		t.Errorf("generated product files: %v\n"+
			"A guard violation inside generated code is a report against the generator, not something a "+
			"contributor can fix in place. Point the guard at the template, or exclude the output and say so "+
			"in docs/testing.md.", generated)
	}
}

// TestGuardEligibility_UnbuiltVariantsDoNotReadTheEnvironment covers the half of
// the eligibility question that has teeth. A file whose constraint the current
// build does not satisfy is invisible to the environment guard, so an
// os.Getenv in it would be reported by nothing at all.
//
// The rule is deliberately not "widen this list when a variant needs one". If a
// non-Linux variant genuinely has to read the environment, the fix is to run
// the environment guard under that GOOS, where it can judge the call properly.
func TestGuardEligibility_UnbuiltVariantsDoNotReadTheEnvironment(t *testing.T) {
	t.Parallel()

	root := reporoot.Path(t)
	checked := 0

	for rel, tag := range buildConstrainedProductFiles() {
		if satisfiedByCurrentBuild(t, tag) {
			continue
		}

		checked++

		body, err := reporoot.ReadFileAt(root, rel)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}

		for _, call := range environmentReadsIn(t, rel, string(body)) {
			t.Errorf("%s reads the environment via %s, but //go:build %s excludes it from the environment "+
				"guard on %s, so nothing checks that call. Run the guard under a matching GOOS instead of "+
				"leaving the read unpoliced.", rel, call, tag, runtime.GOOS)
		}
	}

	if checked == 0 {
		t.Fatalf("no declared variant is excluded on %s; this check measured nothing", runtime.GOOS)
	}
}

// walkProductGoFiles visits the non-test Go files under dir, which is the
// selection every guard in this package makes.
func walkProductGoFiles(t *testing.T, dir, root string, visit func(rel, source string)) {
	t.Helper()

	if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !isProductGoFile(entry, path) {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		body, readErr := reporoot.ReadFileAt(root, rel)
		if readErr != nil {
			return readErr
		}

		visit(filepath.ToSlash(rel), string(body))

		return nil
	}); err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
}

// buildConstraintOf returns the //go:build expression, read from the parsed
// comment groups above the package clause so a constraint written inside a
// string, or too late to count, is judged the way the toolchain judges it.
func buildConstraintOf(t *testing.T, name, source string) (string, bool) {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), name, source, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}

	for _, group := range file.Comments {
		if group.Pos() > file.Package {
			break
		}

		for _, comment := range group.List {
			if expr, ok := strings.CutPrefix(comment.Text, "//go:build "); ok {
				return strings.TrimSpace(expr), true
			}
		}
	}

	return "", false
}

// satisfiedByCurrentBuild evaluates a //go:build expression the way the
// toolchain would for this GOOS, using go/build/constraint rather than a
// hand-rolled reading of the string.
func satisfiedByCurrentBuild(t *testing.T, expr string) bool {
	t.Helper()

	parsed, err := constraint.Parse("//go:build " + expr)
	if err != nil {
		t.Fatalf("parse constraint %q: %v", expr, err)
	}

	unixGOOS := []string{
		"aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos",
		"ios", "linux", "netbsd", "openbsd", "solaris",
	}

	return parsed.Eval(func(tag string) bool {
		return tag == runtime.GOOS || tag == runtime.GOARCH ||
			(tag == "unix" && slices.Contains(unixGOOS, runtime.GOOS))
	})
}

// environmentReadsIn returns the os/syscall environment calls in a file, by
// selector name. The receiver is checked so a method of the same name on an
// application type is not mistaken for one.
func environmentReadsIn(t *testing.T, name, source string) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), name, source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}

	var reads []string

	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}

		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		pkg, ok := selector.X.(*ast.Ident)
		if !ok || (pkg.Name != "os" && pkg.Name != "syscall") {
			return true
		}

		if slices.Contains(envReadingSelectors(), selector.Sel.Name) {
			reads = append(reads, pkg.Name+"."+selector.Sel.Name)
		}

		return true
	})

	sort.Strings(reads)

	return reads
}

// hasGeneratedMarker applies the convention cmd/go and every Go tool uses: the
// marker is a line comment before the package clause matching the fixed form.
func hasGeneratedMarker(source string) bool {
	body, _, found := strings.Cut(source, "\npackage ")
	if !found {
		return false
	}

	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "// Code generated ") && strings.HasSuffix(line, " DO NOT EDIT.") {
			return true
		}
	}

	return false
}
