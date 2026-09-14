// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package mockbinary_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

// mockbinary is the most widely used double in the suite -- 29 test files
// across 23 packages -- so what it needs from the host is worth stating
// where a maintainer will find it.
//
// It needed two things and now needs one. Each stub used to build a JSON
// recording by shelling out to jq:
//
//	printf '"args":%s,' "$(printf '%s\n' "$@" | jq -R . | jq -s -c .)"
//
// so every one of those packages needed `jq` on PATH, declared nowhere --
// not in .mise.toml, not in docs/testing.md, not in the justfile. It was
// present in the runtime image and absent from a plain golang:alpine.
//
// That encoding was also lossy in three ways, which is why it is gone
// rather than merely declared: the argument splitting above turned one
// argument containing a newline into two, a call with no arguments came
// back as one empty argument, and `pwd | jq -Rs .` kept the trailing
// newline on every recorded cwd. The replacement writes NUL-delimited
// fields with printf, which is a bash builtin, so the jq dependency went
// away with the bug that motivated it.
//
// bash remains: every stub is a bash script, and the recording is written
// by its printf.
func TestMockbinary_RequiresItsHostDependencies(t *testing.T) {
	t.Parallel()

	for _, dep := range []struct {
		bin  string
		what string
	}{
		{bin: "bash", what: "every stub is a bash script (#!/usr/bin/env bash) and records with its builtin printf"},
	} {
		if _, err := exec.LookPath(dep.bin); err != nil {
			t.Errorf("mockbinary needs %s on PATH: %s.\n"+
				"Without it every test using this helper fails without naming %s.",
				dep.bin, dep.what, dep.bin)
		}
	}
}

// TestMockbinary_NoLongerNeedsJq is the control for the paragraph above.
//
// "jq is not required" is a claim that rots silently: someone adds a jq call
// to a stub, it works on every machine that has jq, and the dependency is
// back without anyone deciding to reintroduce it. The generated stub is
// checked for the tools it invokes instead of trusting the comment.
func TestMockbinary_NoLongerNeedsJq(t *testing.T) {
	// Not parallel: mockbinary.New sets PATH with t.Setenv.
	stub := generatedStubBody(t, "probe", "exit 0")

	for _, banned := range []string{"jq", "python", "perl", "base64", "xxd", "od"} {
		if containsWord(stub, banned) {
			t.Errorf("the generated stub invokes %q; the recording must use shell builtins only:\n%s", banned, stub)
		}
	}
}

// generatedStubBody returns the script mockbinary writes for a stub.
func generatedStubBody(t *testing.T, name, script string) string {
	t.Helper()

	m := mockbinary.New(t)
	m.Add(name, script)

	body, err := os.ReadFile(m.Path(name))
	if err != nil {
		t.Fatalf("read generated stub: %v", err)
	}

	return string(body)
}

// containsWord reports whether body invokes word as a command token, so that
// "jq" does not match inside a comment word like "jsonl".
func containsWord(body, word string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}

		for _, field := range strings.FieldsFunc(line, func(r rune) bool {
			return r == ' ' || r == '\t' || r == '|' || r == '(' || r == ')' || r == '$' || r == '"' || r == ';'
		}) {
			if field == word {
				return true
			}
		}
	}

	return false
}
