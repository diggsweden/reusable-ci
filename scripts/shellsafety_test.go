// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package scripts

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// The question this file answers is narrow and worth stating exactly.
//
// `printf "-n"` does not print "-n". printf consumes leading-dash words as its
// own options, so a format string that begins with a dash is either silently
// swallowed or turned into an error, depending on the shell. In a diagnostic
// path that means the message a human needed never appears. The fix is
// `printf -- "-n"`, or a format that does not lead with a dash.
//
// The previous guard matched the regex `printf\s+"-[^-]` line by line. It could
// not see `printf '-n'`, `printf -n`, a format continued onto the next line, or
// a second printf on a line whose first one was safe; and it did see the same
// text inside a comment or a here-doc body. Both halves of that are failures:
// the misses let real bugs through, and the false positives are what makes a
// contributor add a `# nolint`-shaped workaround to a guard.
//
// Parsing removes the guesswork. syntax.Parser resolves what is a command, what
// is that command's first word, and what is data. A comment is not a command
// node. A here-doc body is a word, not a call. `$(printf '-x')` inside a
// substitution IS a call, and is checked like any other.

// unsafePrintf is one finding: a printf whose format word starts with a dash
// without the `--` end-of-options marker before it.
type unsafePrintf struct {
	Line uint
	Text string
}

// unsafePrintfCalls parses body as bash and returns every printf call whose
// format argument would be eaten as an option.
//
// A parse error is returned, never swallowed. A script this guard cannot parse
// is a script it cannot vouch for, and reporting nothing for it is exactly the
// fail-open the old regex had.
func unsafePrintfCalls(name string, body []byte) ([]unsafePrintf, error) {
	if strings.HasSuffix(name, ".sh.tmpl") {
		body = renderShellTemplate(body)
	}

	file, err := syntax.NewParser(syntax.KeepComments(false), syntax.Variant(syntax.LangBash)).
		Parse(bytes.NewReader(body), name)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}

	var findings []unsafePrintf

	syntax.Walk(file, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}

		if commandName(call.Args[0]) != "printf" {
			return true
		}

		if finding, unsafe := firstFormatIsDashLed(call); unsafe {
			findings = append(findings, finding)
		}

		return true
	})

	return findings, nil
}

// firstFormatIsDashLed inspects the words after `printf`.
//
// printf's own options come first: bash accepts `-v NAME` to assign instead of
// print, and `--` ends the option list. Those are skipped, because `printf -v
// out '%s' "$x"` is not what this guard is looking for. The first word that is
// neither is the format, and a format whose literal text starts with a dash is
// the bug — unless a `--` already ended the options, which is the fix.
func firstFormatIsDashLed(call *syntax.CallExpr) (unsafePrintf, bool) {
	args := call.Args[1:]

	for index := 0; index < len(args); index++ {
		text, literal := wordText(args[index])

		// A non-literal word (a variable, a substitution) cannot be judged
		// statically. Neither this guard nor the regex it replaced reports one;
		// saying so is better than implying the check is exhaustive.
		if !literal {
			return unsafePrintf{}, false
		}

		if text == "--" {
			return unsafePrintf{}, false
		}

		// `-v NAME` takes an argument; skip both so NAME is not mistaken for
		// the format.
		if text == "-v" {
			index++

			continue
		}

		if strings.HasPrefix(text, "-") && len(text) > 1 && !strings.Contains(text, "%") {
			// A bare option cluster, e.g. `printf -v` with no name. Keep
			// scanning rather than calling it a format.
			if isPrintfOptionCluster(text) {
				continue
			}
		}

		if strings.HasPrefix(text, "-") {
			return unsafePrintf{Line: args[index].Pos().Line(), Text: text}, true
		}

		return unsafePrintf{}, false
	}

	return unsafePrintf{}, false
}

// isPrintfOptionCluster reports a word that is only printf's own option
// letters, so it is not the format. `-v` is the sole option bash's printf
// takes; anything else beginning with a dash is a format the shell would eat.
func isPrintfOptionCluster(text string) bool {
	return text == "-v"
}

// commandName returns the literal command name of a word, or "" when the word
// is not a plain literal (a variable holding a command name, say).
func commandName(word *syntax.Word) string {
	text, literal := wordText(word)
	if !literal {
		return ""
	}

	return text
}

// wordText renders a word's literal text. It reports false when any part of the
// word is an expansion, a substitution or arithmetic, because such a word's
// value is not known statically.
func wordText(word *syntax.Word) (string, bool) {
	var out strings.Builder

	for _, part := range word.Parts {
		switch typed := part.(type) {
		case *syntax.Lit:
			out.WriteString(typed.Value)
		case *syntax.SglQuoted:
			out.WriteString(typed.Value)
		case *syntax.DblQuoted:
			for _, inner := range typed.Parts {
				lit, ok := inner.(*syntax.Lit)
				if !ok {
					return "", false
				}

				out.WriteString(lit.Value)
			}
		default:
			return "", false
		}
	}

	return out.String(), true
}

// placeholderPattern matches the probe templates' substitution markers.
var placeholderPattern = regexp.MustCompile(`__[A-Z][A-Z0-9_]*__`)

// renderShellTemplate makes a `.sh.tmpl` probe parsable without knowing what it
// will be filled with.
//
// The templates in internal/livetest/probes substitute two different kinds of
// thing through the same `__NAME__` spelling. A marker at the start of a line
// expands to whole statements — `__TOKEN_FETCH__if [ -z ... ]` is a block of
// shell followed by an `if` — so it is dropped. A marker anywhere else is a
// value, as in `-o credential-proxy "__PROXY_ASSET_URL__"`, so it becomes a
// literal.
//
// That is a reading of the templates, not a guarantee about them, and it is
// worth being plain about the consequence: a printf inside the substituted text
// is checked where that text lives (inrunnerprobes.go and the probes it builds),
// not here. What this does buy is that the rest of each template is parsed
// rather than skipped, which is what the previous line-based scan could not do
// and what skipping templates entirely would give up.
func renderShellTemplate(body []byte) []byte {
	lines := strings.Split(string(body), "\n")

	for i, line := range lines {
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		rest := line[len(indent):]

		for {
			loc := placeholderPattern.FindStringIndex(rest)
			if loc == nil || loc[0] != 0 {
				break
			}

			rest = rest[loc[1]:]
		}

		lines[i] = indent + placeholderPattern.ReplaceAllString(rest, "rci_placeholder")
	}

	return []byte(strings.Join(lines, "\n"))
}
