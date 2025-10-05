// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archguard

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// Fixtures are parsed, never compiled as adapters or used to start processes.
// Each @ marks an exact offending process call, not a package-wide failure.
func TestExecWrapFixtures(t *testing.T) {
	t.Parallel()

	const imports = `package fixture
import (
 "github.com/diggsweden/reusable-ci/v3/internal/safeexec"
 "os/exec"
 "errors"
 "fmt"
)
`

	for _, tc := range []struct {
		name, body string
		checked    int
	}{
		{"direct run", `func run() error { cmd := safeexec.Command(ctx, bin); err := cmd.Run(); return safeexec.WrapError(err, bin, "") }`, 1},
		{"inline run", `func run() error { cmd := safeexec.Command(ctx, bin); return safeexec.WrapError(cmd.Run(), bin, "") }`, 1},
		{"combined output", `func run() error { _, err := safeexec.Command(ctx, bin).CombinedOutput(); return safeexec.WrapError(err, bin, "") }`, 1},
		{"output", `func run() error { cmd := safeexec.Command(ctx, bin); _, err := cmd.Output(); return safeexec.WrapError(err, bin, "") }`, 1},
		{"start and wait", `func run() error { cmd := safeexec.Command(ctx, bin); if err := cmd.Start(); err != nil { return safeexec.WrapError(err, bin, "") }; return safeexec.WrapError(cmd.Wait(), bin, "") }`, 2},
		{"wait gap", `func run() error { cmd := safeexec.Command(ctx, bin); if err := cmd.Start(); err != nil { return safeexec.WrapError(err, bin, "") }; return @cmd.Wait() }`, 2},
		{"if initializer", `func run() error { cmd := safeexec.Command(ctx, bin); if err := cmd.Run(); err != nil { return safeexec.WrapError(err, bin, "") }; return nil }`, 1},
		{"exit code adapter", `func run() (int, error) { cmd := safeexec.Command(ctx, bin); err := cmd.Run(); if err == nil { return 0, nil }; var exitErr *exec.ExitError; if errors.As(err, &exitErr) { return exitErr.ExitCode(), nil }; return -1, safeexec.WrapError(err, bin, "") }`, 1},
		{"git fallback classification", `func run() (bool, error) { cmd := safeexec.Command(ctx, bin); err := cmd.Run(); if err == nil { return true, nil }; var exitErr *exec.ExitError; if errors.As(err, &exitErr) { if exitErr.ExitCode() == 1 { return false, nil }; return false, fmt.Errorf("exit: %w", sentinel) }; return false, safeexec.WrapError(err, bin, "") }`, 1},
		{"non exit fallback gap", `func run() error { cmd := safeexec.Command(ctx, bin); err := @cmd.Run(); if err == nil { return nil }; var exitErr *exec.ExitError; if errors.As(err, &exitErr) { return safeexec.WrapError(err, bin, "") }; return err }`, 1},
		{"negated exit branch", `func run() error { cmd := safeexec.Command(ctx, bin); err := cmd.Run(); if nil == err { return nil }; var exitErr *exec.ExitError; if !errors.As(err, &exitErr) { return safeexec.WrapError(err, bin, "") }; return nil }`, 1},
		{"stderr helper", `func run() error { cmd := safeexec.Command(ctx, bin); err := cmd.Run(); return safeexec.WrapErrorWithStderr(err, bin, "", stderr) }`, 1},
		{"wrapped result branches", `func run() error { cmd := safeexec.Command(ctx, bin); out, err := cmd.CombinedOutput(); if err != nil { wrapped := safeexec.WrapError(err, bin, ""); if len(out) == 0 { return wrapped }; return fmt.Errorf("%w\n%s", wrapped, out) }; return nil }`, 1},
		{"function gap", `func covered() error { cmd := safeexec.Command(ctx, bin); return safeexec.WrapError(cmd.Run(), bin, "") }
func run() error { cmd := safeexec.Command(ctx, bin); return @cmd.Run() }`, 2},
		{"second process gap", `func run() error { first := safeexec.Command(ctx, bin); err := first.Run(); if err != nil { return safeexec.WrapError(err, bin, "") }; second := safeexec.Command(ctx, bin); return @second.Run() }`, 2},
		{"wrong process error", `func run() error { first := safeexec.Command(ctx, bin); firstErr := @first.Run(); second := safeexec.Command(ctx, bin); secondErr := second.Run(); _ = firstErr; return safeexec.WrapError(secondErr, bin, "") }`, 2},
		{"branch gap", `func run() error { cmd := safeexec.Command(ctx, bin); err := @cmd.Run(); if flag { return safeexec.WrapError(err, bin, "") }; return err }`, 1},
		{"sibling branch gap", `func run() error { if flag { cmd := safeexec.Command(ctx, bin); return safeexec.WrapError(cmd.Run(), bin, "") } else { cmd := safeexec.Command(ctx, bin); return @cmd.Run() } }`, 2},
		{"ignored wrapper", `func run() error { cmd := safeexec.Command(ctx, bin); err := @cmd.Run(); _ = safeexec.WrapError(err, bin, ""); return err }`, 1},
		{"later wrapper", `func run() error { cmd := safeexec.Command(ctx, bin); err := @cmd.Run(); return err; return safeexec.WrapError(err, bin, "") }`, 1},
		{"inert wrap tokens", "func run() error { cmd := safeexec.Command(ctx, bin); err := @cmd.Run(); // safeexec.WrapError(err, bin, \"\")\n _ = `safeexec.WrapError(err, bin, \"\")`; return err }", 1},
		{"inert command tokens", "// safeexec.Command(ctx, bin)\nvar text = `safeexec.Command(ctx, bin)`", 0},
		{"closure cannot cover outer", `func run() error { cmd := safeexec.Command(ctx, bin); err := @cmd.Run(); _ = func() error { return safeexec.WrapError(err, bin, "") }; return err }`, 1},
		{"closure process discovered", `func run() { _ = func() error { cmd := safeexec.Command(ctx, bin); return @cmd.Run() } }`, 1},
		{"local aliases", `func run() error { makeCmd := safeexec.Command; wrap := (safeexec.WrapError); cmd := makeCmd(ctx, bin); alias := cmd; err := alias.Run(); saved := err; return wrap(saved, bin, "") }`, 1},
		{"method alias", `func run() error { cmd := safeexec.Command(ctx, bin); invoke := cmd.Run; err := invoke(); return safeexec.WrapError(err, bin, "") }`, 1},
		{"method alias gap", `func run() error { cmd := safeexec.Command(ctx, bin); invoke := cmd.Run; return @invoke() }`, 1},
		{"var output declaration", `func run() error { cmd := safeexec.Command(ctx, bin); var out, err = cmd.Output(); _ = out; return safeexec.WrapError(err, bin, "") }`, 1},
		{"wrapper alias overwritten", `func run() error { wrap := safeexec.WrapError; wrap = fake; cmd := safeexec.Command(ctx, bin); err := @cmd.Run(); return wrap(err, bin, "") }`, 1},
		{"error alias overwritten", `func run() error { cmd := safeexec.Command(ctx, bin); err := @cmd.Run(); saved := err; saved = other; return safeexec.WrapError(saved, bin, "") }`, 1},
		{"import shadow", `func run() error { cmd := safeexec.Command(ctx, bin); err := @cmd.Run(); safeexec := fake; return safeexec.WrapError(err, bin, "") }`, 1},
		{"parameter shadow", `func run(safeexec Fake) error { cmd := safeexec.Command(ctx, bin); return cmd.Run() }`, 0},
		{"error shadow", `func run() error { cmd := safeexec.Command(ctx, bin); err := @cmd.Run(); { err := other; _ = safeexec.WrapError(err, bin, "") }; return err }`, 1},
		{"conditional reassignment", `func run() error { cmd := safeexec.Command(ctx, bin); err := @cmd.Run(); wrapped := safeexec.WrapError(err, bin, ""); if flag { wrapped = err }; return wrapped }`, 1},
		{"conditional wrapping", `func run() error { cmd := safeexec.Command(ctx, bin); err := @cmd.Run(); wrapped := err; if flag { wrapped = safeexec.WrapError(err, bin, "") }; return wrapped }`, 1},
		{"both branches wrap", `func run() error { cmd := safeexec.Command(ctx, bin); err := cmd.Run(); wrapped := err; if flag { wrapped = safeexec.WrapError(err, bin, "one") } else { wrapped = safeexec.WrapError(err, bin, "two") }; return wrapped }`, 1},
		{"fmt must preserve sentinel", `func run() error { cmd := safeexec.Command(ctx, bin); err := @cmd.Run(); return fmt.Errorf("%v", safeexec.WrapError(err, bin, "")) }`, 1},
		{"unrelated error sink", `func run() error { cmd := safeexec.Command(ctx, bin); err := @cmd.Run(); return discard(safeexec.WrapError(err, bin, "")) }`, 1},
		{"wrong errors As", `func run() error { cmd := safeexec.Command(ctx, bin); err := @cmd.Run(); errors := fake; var exitErr *exec.ExitError; if errors.As(err, &exitErr) { return nil }; return safeexec.WrapError(err, bin, "") }`, 1},
		{"wrong As target", `func run() error { cmd := safeexec.Command(ctx, bin); err := @cmd.Run(); var exitErr *Other; if errors.As(err, &exitErr) { return nil }; return safeexec.WrapError(err, bin, "") }`, 1},
		{"helper parameter", `func finish(err error) error { if err == nil { return nil }; return safeexec.WrapError(err, bin, "") }
func run() error { cmd := safeexec.Command(ctx, bin); err := cmd.Run(); return finish(err) }`, 1},
		{"helper branch gap", `func finish(err error) error { if flag { return safeexec.WrapError(err, bin, "") }; return err }
func run() error { cmd := safeexec.Command(ctx, bin); err := @cmd.Run(); return finish(err) }`, 1},
		{"uncalled helper", `func finish(err error) error { return safeexec.WrapError(err, bin, "") }
func run() error { cmd := safeexec.Command(ctx, bin); return @cmd.Run() }`, 1},
		{"helper wrong argument", `func finish(err error) error { return safeexec.WrapError(err, bin, "") }
func run() error { cmd := safeexec.Command(ctx, bin); err := @cmd.Run(); _ = err; return finish(other) }`, 1},
		{"helper shadow", `func finish(err error) error { return safeexec.WrapError(err, bin, "") }
func run() error { cmd := safeexec.Command(ctx, bin); err := @cmd.Run(); finish := fake; return finish(err) }`, 1},
		{"recursive helper bounded", `func finish(err error) error { return finish(err) }
func run() error { cmd := safeexec.Command(ctx, bin); err := @cmd.Run(); return finish(err) }`, 1},
		{"command builder", `type Adapter struct{}
func (a *Adapter) command() *exec.Cmd { cmd := safeexec.Command(ctx, bin); cmd.Dir = dir; return cmd }
func (a *Adapter) run() error { cmd := a.command(); return safeexec.WrapError(cmd.Run(), bin, "") }`, 1},
		{"command builder gap", `type Adapter struct{}
func (a *Adapter) command() *exec.Cmd { return safeexec.Command(ctx, bin) }
func (a *Adapter) run() error { cmd := a.command(); return @cmd.Run() }`, 1},
		{"uncertain builder retains process site", `type Adapter struct{}
func (a *Adapter) command() *exec.Cmd { if flag { return safeexec.Command(ctx, bin) }; return nil }
func (a *Adapter) run() error { cmd := a.command(); return safeexec.WrapError(@cmd.Run(), bin, "") }`, 1},
		{"unrelated builder receiver", `type Adapter struct{}
func (a *Adapter) command() *exec.Cmd { return safeexec.Command(ctx, bin) }
func run() error { a := fake; cmd := a.command(); return cmd.Run() }`, 0},
		{"loop cannot prove wrap", `func run() error { cmd := safeexec.Command(ctx, bin); err := @cmd.Run(); for flag { err = safeexec.WrapError(err, bin, "") }; return err }`, 1},
		{"loop cannot retain wrapper alias", `func run() error { wrap := safeexec.WrapError; for flag { wrap = fake }; cmd := safeexec.Command(ctx, bin); err := @cmd.Run(); return wrap(err, bin, "") }`, 1},
		{"helper loop raw return", `func finish(err error) error { for flag { return err }; return safeexec.WrapError(err, bin, "") }
func run() error { cmd := safeexec.Command(ctx, bin); return finish(@cmd.Run()) }`, 1},
		{"process in loop discovered", `func run() error { for flag { cmd := safeexec.Command(ctx, bin); return @cmd.Run() }; return nil }`, 1},
		{"process in switch discovered", `func run() error { switch mode { case "run": cmd := safeexec.Command(ctx, bin); return @cmd.Run() }; return nil }`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := imports + tc.body + "\n"

			var want []string

			for strings.Contains(source, "@") {
				mark := strings.IndexByte(source, '@')
				prefix := source[:mark]
				want = append(want, fmt.Sprintf("nested/tool/adapter.go:%d:%d:", strings.Count(prefix, "\n")+1, mark-strings.LastIndex(prefix, "\n")))
				source = source[:mark] + source[mark+1:]
			}

			checked, offenders, err := execAdapterUsage(fstest.MapFS{"nested/tool/adapter.go": {Data: []byte(source)}})
			if err != nil {
				t.Fatal(err)
			}

			if checked != tc.checked || len(offenders) != len(want) {
				t.Fatalf("exec grading: checked %d, offenders %v; want checked %d, locations %v", checked, offenders, tc.checked, want)
			}

			for _, location := range want {
				if !slices.ContainsFunc(offenders, func(offender string) bool { return strings.HasPrefix(offender, location) }) {
					t.Errorf("exec grading: missing exact offending location %s in %v", location, offenders)
				}
			}
		})
	}
}

func TestExecWrapTreeFixtures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		files   map[string]string
		checked int
		want    []string
	}{
		{"cross file helper and import aliases", map[string]string{
			"nested/deeper/adapter.go": `package p
import sx "github.com/diggsweden/reusable-ci/v3/internal/safeexec"
func run() error { cmd := sx.Command(ctx, bin); err := cmd.Run(); return finish(err) }
`,
			"nested/deeper/helper.go": `package p
import sx "github.com/diggsweden/reusable-ci/v3/internal/safeexec"
func finish(err error) error { return sx.WrapError(err, bin, "") }
`,
		}, 1, nil},
		{"per file import identity", map[string]string{
			"adapter.go": `package p
import sx "github.com/diggsweden/reusable-ci/v3/internal/safeexec"
func run() error { cmd := sx.Command(ctx, bin); err := cmd.Run(); return finish(err) }
`,
			"helper.go": `package p
import sx "example.org/safeexec"
func finish(err error) error { return sx.WrapError(err, bin, "") }
`,
		}, 1, []string{"adapter.go:3:56: run: Run"}},
		{"dot import", map[string]string{"adapter.go": `package p
import . "github.com/diggsweden/reusable-ci/v3/internal/safeexec"
func run() error { cmd := Command(ctx, bin); return WrapError(cmd.Run(), bin, "") }
`}, 1, nil},
		{"dot shadow", map[string]string{"adapter.go": `package p
import . "github.com/diggsweden/reusable-ci/v3/internal/safeexec"
func run() error { cmd := Command(ctx, bin); WrapError := fake; return WrapError(cmd.Run(), bin, "") }
		`}, 1, []string{"adapter.go:3:82: run: Run"}},
		{"unimported spelling", map[string]string{"adapter.go": `package p
func run() error { cmd := safeexec.Command(ctx, bin); return cmd.Run() }
`}, 0, nil},
		{"wrong import", map[string]string{"adapter.go": `package p
import "example.org/safeexec"
func run() error { cmd := safeexec.Command(ctx, bin); return cmd.Run() }
`}, 0, nil},
		{"nested and test exclusion", map[string]string{
			"good/adapter.go": `package p
import "github.com/diggsweden/reusable-ci/v3/internal/safeexec"
func run() error { cmd := safeexec.Command(ctx, bin); return safeexec.WrapError(cmd.Run(), bin, "") }
`,
			"good/nested/bad.go": `package p
import "github.com/diggsweden/reusable-ci/v3/internal/safeexec"
func broken() error { cmd := safeexec.Command(ctx, bin); return cmd.Run() }
`,
			"good/nested/ignored_test.go": "not even Go syntax",
			"good/nested/ignored.txt":     "not Go syntax either",
		}, 2, []string{"good/nested/bad.go:3:65: broken: Run"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tree := fstest.MapFS{}
			for name, source := range tc.files {
				tree[name] = &fstest.MapFile{Data: []byte(source)}
			}

			checked, offenders, err := execAdapterUsage(tree)
			if err != nil {
				t.Fatal(err)
			}

			if checked != tc.checked || len(offenders) != len(tc.want) {
				t.Fatalf("exec tree grading: checked %d, offenders %v; want %d, %v", checked, offenders, tc.checked, tc.want)
			}

			for i, want := range tc.want {
				if !strings.HasPrefix(offenders[i], want+" ") {
					t.Errorf("exec tree grading: got %q; want location/function prefix %q", offenders[i], want)
				}
			}
		})
	}
}

func TestExecWrapMalformedFixture(t *testing.T) {
	t.Parallel()

	_, _, err := execAdapterUsage(fstest.MapFS{"nested/broken.go": {Data: []byte("not Go")}})
	if err == nil || !strings.Contains(err.Error(), "nested/broken.go") {
		t.Fatalf("malformed adapter must fail with its location, got %v", err)
	}
}

// Unlike the deliberately partial snippets above, these review regressions are
// complete Go sources, suitable for compile-only checks against a safeexec stub.
func TestExecWrapReviewFixtures(t *testing.T) {
	t.Parallel()

	const (
		prefix = `package fixture
import (
 "context"
 "fmt"
 "os/exec"
 "github.com/diggsweden/reusable-ci/v3/internal/safeexec"
)
var ctx context.Context
const bin = "tool"
var _ = fmt.Errorf
var _ *exec.Cmd
`
		helperPrefix = `package fixture
import "github.com/diggsweden/reusable-ci/v3/internal/safeexec"
`
	)
	for _, tc := range []struct {
		name, body, function, reason string
		helpers                      map[string]string
		clean                        bool
	}{
		{
			name: "short circuit operand",
			body: `func run(flag bool) error {
 cmd := safeexec.Command(ctx, bin)
 if flag && @cmd.Run() != nil { return nil }
 return nil
}`,
			function: "run", reason: "unsupported expression",
		},
		{
			name: "right comparison operand",
			body: `func run(other error) bool {
 cmd := safeexec.Command(ctx, bin)
 return other == @cmd.Run()
}`,
			function: "run", reason: "unsupported expression",
		},
		{
			name: "selector receiver operand",
			body: `func run() string {
 cmd := safeexec.Command(ctx, bin)
 return @cmd.Run().Error()
}`,
			function: "run", reason: "unsupported expression",
		},
		{
			name: "indexed composite operand",
			body: `func run() error {
 cmd := safeexec.Command(ctx, bin)
 return []error{@cmd.Run()}[0]
}`,
			function: "run", reason: "unsupported expression",
		},
		{
			name: "generic receiver identity",
			body: `type Covered[T any] struct{}
type Broken[T any] struct{}
func (a *Covered[T]) finish(err error) error { return safeexec.WrapError(err, bin, "") }
func (a *Broken[T]) finish(err error) error { return err }
func (a *Broken[T]) run() error {
 cmd := safeexec.Command(ctx, bin)
 err := @cmd.Run()
 return a.finish(err)
}`,
			function: "Broken.run",
		},
		{
			name: "generic covered helper control",
			body: `type Covered[T any] struct{}
type Broken[T any] struct{}
func (a *Covered[T]) finish(err error) error { return safeexec.WrapError(err, bin, "") }
func (a *Broken[T]) finish(err error) error { return err }
func (a *Covered[T]) run() error {
 cmd := safeexec.Command(ctx, bin)
 err := @cmd.Run()
 return a.finish(err)
}`,
			function: "Covered.run", clean: true,
		},
		{
			name: "multi parameter receiver identity",
			body: `type Covered[A, B any] struct{}
type Broken[A, B any] struct{}
func (a *Covered[A, B]) finish(err error) error { return safeexec.WrapError(err, bin, "") }
func (a *Broken[A, B]) finish(err error) error { return err }
func (a *Broken[A, B]) run() error {
 cmd := safeexec.Command(ctx, bin)
 err := @cmd.Run()
 return a.finish(err)
}`,
			function: "Broken.run",
		},
		{
			name: "generic command builder control",
			body: `type Adapter[A, B any] struct{}
func (a *Adapter[A, B]) command() *exec.Cmd { return safeexec.Command(ctx, bin) }
func (a *Adapter[A, B]) run() error {
 cmd := a.command()
 return safeexec.WrapError(@cmd.Run(), bin, "")
}`,
			function: "Adapter.run", clean: true,
		},
		{
			name: "ambiguous platform helper",
			body: `func run() error {
 cmd := safeexec.Command(ctx, bin)
 return finish(@cmd.Run())
}`,
			function: "run", reason: "ambiguous helper declaration",
			helpers: map[string]string{
				"helper_darwin.go": helperPrefix + `func finish(err error) error { return safeexec.WrapError(err, bin, "") }`,
				"helper_linux.go":  `package fixture; func finish(err error) error { return err }`,
			},
		},
		{
			name: "ambiguous platform helper reversed",
			body: `func run() error {
 cmd := safeexec.Command(ctx, bin)
 return finish(@cmd.Run())
}`,
			function: "run", reason: "ambiguous helper declaration",
			helpers: map[string]string{
				"helper_darwin.go": `package fixture; func finish(err error) error { return err }`,
				"helper_linux.go":  helperPrefix + `func finish(err error) error { return safeexec.WrapError(err, bin, "") }`,
			},
		},
		{
			name: "ambiguous platform builder",
			body: `func run() error {
 cmd := command()
 return safeexec.WrapError(@cmd.Run(), bin, "")
}`,
			function: "run", reason: "ambiguous helper declaration",
			helpers: map[string]string{
				"helper_darwin.go": helperPrefix + `import "os/exec"
func command() *exec.Cmd { return safeexec.Command(ctx, bin) }`,
				"helper_linux.go": `package fixture; import "os/exec"
func command() *exec.Cmd { return nil }`,
			},
		},
		{
			name: "loop initializer command",
			body: `func run(flag bool) error {
 for cmd := safeexec.Command(ctx, bin); flag; {
  return safeexec.WrapError(@cmd.Run(), bin, "")
 }
 return nil
}`,
			function: "run", reason: "unsupported control flow",
		},
		{
			name: "switch initializer command",
			body: `func run(flag bool) error {
 switch cmd := safeexec.Command(ctx, bin); flag {
 default: return safeexec.WrapError(@cmd.Run(), bin, "")
 }
}`,
			function: "run", reason: "unsupported control flow",
		},
		{
			name: "type switch initializer command",
			body: `func run(value any) error {
 switch cmd := safeexec.Command(ctx, bin); value.(type) {
 default: return safeexec.WrapError(@cmd.Run(), bin, "")
 }
}`,
			function: "run", reason: "unsupported control flow",
		},
		{
			name: "escaped percent is not wrapping",
			body: `func run() error {
 cmd := safeexec.Command(ctx, bin)
 err := @cmd.Run()
 return fmt.Errorf("%%w", nil, safeexec.WrapError(err, bin, ""))
}`,
			function: "run", reason: "unsupported error format",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := prefix + tc.body + "\n"

			mark := strings.IndexByte(source, '@')
			if mark < 0 || strings.Count(source, "@") != 1 {
				t.Fatal("review fixture must mark exactly one offending process")
			}

			want := fmt.Sprintf("review/adapter.go:%d:%d: %s: Run has an unclassified or unsupported non-exit failure path",
				strings.Count(source[:mark], "\n")+1, mark-strings.LastIndex(source[:mark], "\n"), tc.function)
			if tc.reason != "" {
				want += ": " + tc.reason
			}

			tree := fstest.MapFS{"review/adapter.go": {Data: []byte(strings.ReplaceAll(source, "@", ""))}}
			for name, helper := range tc.helpers {
				tree["review/"+name] = &fstest.MapFile{Data: []byte(helper)}
			}

			checked, offenders, err := execAdapterUsage(tree)
			if err != nil {
				t.Fatal(err)
			}

			wants := []string{want}
			if tc.clean {
				wants = nil
			}

			if checked != 1 || !slices.Equal(offenders, wants) {
				t.Fatalf("exec review grading: checked %d, offenders %q; want checked 1, exact sites/reasons %q", checked, offenders, wants)
			}
		})
	}
}
