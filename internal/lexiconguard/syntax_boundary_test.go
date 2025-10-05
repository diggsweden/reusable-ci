// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package lexiconguard

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDigestRegexpGuardSyntax(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{bareSHA256HexPattern, prefixedSHA256DigestPattern} {
		t.Run(pattern, func(t *testing.T) {
			quoted := strconv.Quote(pattern)
			for _, tc := range []struct {
				name   string
				source string
				want   bool
			}{
				{"quoted", `import "regexp"; var _ = regexp.MustCompile(` + quoted + `)`, true},
				{"raw", "import \"regexp\"; var _ = regexp.Compile(`" + pattern + "`)", true},
				{"alias", `import rx "regexp"; var _ = rx.MustCompilePOSIX(` + quoted + `)`, true},
				{"dot", `import . "regexp"; var _ = CompilePOSIX(` + quoted + `)`, true},
				{"match", `import "regexp"; var _, _ = regexp.Match(` + quoted + `, nil)`, true},
				{"match-string", `import "regexp"; var _, _ = regexp.MatchString(` + quoted + `, "")`, true},
				{"match-reader", `import "regexp"; var _, _ = regexp.MatchReader(` + quoted + `, nil)`, true},
				{"constant", `import "regexp"; const p string = ` + quoted + `; var _ = regexp.MustCompile(p)`, true},
				{"inherited-constant", `import "regexp"; const (first = ` + quoted + `; second); var _ = regexp.MustCompile(second)`, true},
				{"inherited-pair", `import "regexp"; const (first, other = "safe", ` + quoted + `; second, pattern); var _ = regexp.MustCompile(pattern)`, true},
				{"concatenated", `import "regexp"; const p = "^"; var _ = regexp.MustCompile((p + ` + strconv.Quote(pattern[1:]) + `))`, true},
				{"escaped", `import "regexp"; var _ = regexp.MustCompile("\x5e` + pattern[1:] + `")`, true},
				{"line-comment", "// regexp.MustCompile(" + quoted + ")\n", false},
				{"block-comment", "/* regexp.MustCompile(" + quoted + ") */", false},
				{"inert-string", `const example = ` + quoted, false},
				{"inert-code", `const example = ` + strconv.Quote("regexp.MustCompile("+quoted+")"), false},
				{"logged-example", `import "fmt"; func f() { fmt.Println(` + quoted + `) }`, false},
				{"shadowed-import", `import "regexp"; func f(regexp custom) { regexp.MustCompile(` + quoted + `) }`, false},
				{"shadowed-dot", `import . "regexp"; func f(MustCompile func(string)) { MustCompile(` + quoted + `) }`, false},
				{"not-stdlib", `import "example.invalid/regexp"; var _ = regexp.MustCompile(` + quoted + `)`, false},
				{"different-pattern", `import "regexp"; var _ = regexp.MustCompile("^[0-9]+$")`, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					root := t.TempDir()
					require.NoError(t, os.WriteFile(filepath.Join(root, "candidate.go"), []byte("package fixture\n"+tc.source), 0o600))

					rule := singleSource{root: root, patterns: []string{pattern}, regexpCalls: true}
					if tc.want {
						require.Equal(t, []string{"candidate.go"}, rule.offenders(t))
					} else {
						require.Empty(t, rule.offenders(t))
					}
				})
			}
		})
	}
}

func TestRetryWaitGuardSyntax(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		imports string
		body    string
		want    int
	}{
		{"linear", `import "time"`, `for attempt := 1; attempt < 4; attempt++ { if err := op(); err == nil { return }; time.Sleep(time.Duration(attempt) * delay) }`, 1},
		{"reordered", `import "time"`, `for n := 1; n < 4; n++ { if err := op(); nil == err { break }; time.Sleep(delay * time.Duration(n)) }`, 1},
		{"inside-conversion", `import "time"`, `for n := 1; n < 4; n++ { if err := op(); err == nil { return }; <-time.After(time.Duration(delay * n)) }`, 1},
		{"intermediate-and-alias", `import clock "time"`, `for n := 0; n < 4; n++ { _, err := op(); if err == nil { return }; wait := delay * clock.Duration(n + 1); clock.Sleep(wait) }`, 1},
		{"timer-select", `import "time"`, `for { err := op(); if err == nil { return }; timer := time.NewTimer(delay); select { case <-timer.C: case <-ctx.Done(): return } }`, 1},
		{"dot-import", `import . "time"`, `for { if err := op(); err == nil { break }; Sleep(delay) }`, 1},
		{"canonical-use", `import retry "github.com/diggsweden/reusable-ci/v3/internal/retry"`, `retry.Run(ctx, nil, 4, delay, op)`, 0},
		{"canonical-not-an-exemption", `import ("time"; "github.com/diggsweden/reusable-ci/v3/internal/retry")`, `retry.Run(ctx, nil, 4, delay, op); for { if err := op(); err == nil { return }; time.Sleep(delay) }`, 1},
		{"unrelated-conversion", `import "time"`, `_ = time.Duration(attempts); for { if err := op(); err == nil { return }; _ = time.Duration(attempts) }`, 0},
		{"pacing-not-retry", `import "time"`, `for n := range 4 { time.Sleep(time.Duration(n) * delay) }`, 0},
		{"no-loop", `import "time"`, `if err := op(); err == nil { return }; time.Sleep(delay)`, 0},
		{"call-outside-loop", `import "time"`, `err := op(); for { if err == nil { return }; time.Sleep(delay) }`, 0},
		{"timer-outside-loop", `import "time"`, `timer := time.NewTimer(delay); for { if err := op(); err == nil { return }; <-timer.C }`, 0},
		{"inert", "", "_ = `for { if err := op(); err == nil { return }; time.Sleep(delay) }`; /* time.Duration(attempt) */", 0},
		{"shadowed-import", `import "time"`, `time := fakeClock(); for { if err := op(); err == nil { return }; time.Sleep(delay) }`, 0},
		{"shadowed-nil", `import "time"`, `nil := sentinel; for { if err := op(); err == nil { return }; time.Sleep(delay) }`, 0},
		{"shadowed-timer", `import "time"`, `for { if err := op(); err == nil { return }; timer := time.NewTimer(delay); { timer := other(); <-timer.C }; _ = timer }`, 0},
		{"unused-timer", `import "time"`, `for { if err := op(); err == nil { return }; _ = time.NewTimer(delay) }`, 0},
		{"not-a-wait", `import "time"`, `for { if err := op(); err == nil { return }; _ = time.After(delay) }`, 0},
		{"async-and-deferred", `import "time"`, `for { if err := op(); err == nil { return }; go time.Sleep(delay); defer time.Sleep(delay); _ = func() { time.Sleep(delay) } }`, 0},

		// The three shapes D202 recorded as unrecognised. Each is an ordinary
		// way to write the same retry, and each used to pass the guard.
		{"predeclared-result", `import "time"`, `var err error; for n := 0; n < 4; n++ { err = op(); if err == nil { return }; time.Sleep(delay) }`, 1},
		{"reassigned-result", `import "time"`, `err := first(); for { if err == nil { return }; err = op(); time.Sleep(delay) }`, 1},
		{"else-branch-wait", `import "time"`, `for { if err := op(); err == nil { return } else { time.Sleep(delay) } }`, 1},
		{"wait-alias", `import "time"`, `sleep := time.Sleep; for { if err := op(); err == nil { return }; sleep(delay) }`, 1},
		{"aliased-import-and-alias", `import clock "time"`, `sleep := clock.Sleep; for { if err := op(); err == nil { break }; sleep(delay) }`, 1},

		// And the boundaries of those three, so widening the recognizer did not
		// turn ordinary code into a violation.
		{"predeclared-never-reassigned", `import "time"`, `var err error; err = first(); for { if err == nil { return }; time.Sleep(delay) }`, 0},
		{"predeclared-assigned-a-value-not-a-call", `import "time"`, `var err error; for { err = sentinel; if err == nil { return }; time.Sleep(delay) }`, 0},
		{"alias-of-something-else", `import "time"`, `sleep := pace.Sleep; for { if err := op(); err == nil { return }; sleep(delay) }`, 0},
		{"alias-computed-not-followed", `import "time"`, `sleep := pick(time.Sleep); for { if err := op(); err == nil { return }; sleep(delay) }`, 0},
		{"else-branch-of-an-unrelated-guard", `import "time"`, `for n := range 4 { if n > 2 { return } else { time.Sleep(delay) } }`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			source := "package fixture\n" + tc.imports + "\nfunc f() {\n" + tc.body + "\n}"
			file, err := parser.ParseFile(fset, "fixture.go", source, 0)
			require.NoError(t, err)

			positions := retryWaits(file)
			require.Len(t, positions, tc.want)

			for _, pos := range positions {
				require.Equal(t, 4+strings.Count(tc.imports, "\n"), fset.Position(pos).Line)
			}
		})
	}
}
