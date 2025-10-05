// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"regexp"
	"strings"
)

// These checks need literal context-member paths, not an expression evaluator.
// Quoted expression strings are skipped; quoted bracket members are decoded by
// the member grammar. Dynamic indexes deliberately do not claim a static path.
type expressionRef struct {
	members []string
	offset  int
}

//nolint:gochecknoglobals // immutable literal member grammar.
var expressionMember = regexp.MustCompile(`^(?:\s*\.\s*([A-Za-z_][A-Za-z0-9_-]*)|\s*\[\s*'([^']*)'\s*\]|\s*\[\s*"([^"]*)"\s*\])`)

func expressionReferences(text, contextName string, bare bool) []expressionRef {
	if !strings.Contains(text, "${{") {
		if bare {
			return contextMemberRefs(text, contextName, 0)
		}

		return nil
	}

	var refs []expressionRef

	for start := 0; start < len(text); {
		open := strings.Index(text[start:], "${{")
		if open < 0 {
			break
		}

		start += open + 3

		closing := start
		for closing < len(text) && !strings.HasPrefix(text[closing:], "}}") {
			if text[closing] == '\'' || text[closing] == '"' {
				closing = expressionStringEnd(text, closing)
			} else {
				closing++
			}
		}

		if closing == len(text) {
			break
		}

		refs = append(refs, contextMemberRefs(text[start:closing], contextName, start)...)
		start = closing + 2
	}

	return refs
}

func contextMemberRefs(text, contextName string, baseOffset int) []expressionRef { //nolint:cyclop // one bounded lexical walk distinguishes strings, root identifiers and literal member paths.
	var refs []expressionRef

	end := len(text)
	for index := 0; index < end; {
		if text[index] == '\'' || text[index] == '"' {
			index = expressionStringEnd(text, index)

			continue
		}

		if !expressionIdent(text[index]) {
			index++

			continue
		}

		wordStart := index
		for index < end && expressionIdent(text[index]) {
			index++
		}

		if text[wordStart:index] != contextName || strings.HasSuffix(strings.TrimSpace(text[:wordStart]), ".") {
			continue
		}

		var members []string

		for index < end {
			match := expressionMember.FindStringSubmatch(text[index:end])
			if match == nil {
				break
			}

			value := ""

			for _, group := range match[1:] {
				if group != "" {
					value = group

					break
				}
			}

			members = append(members, value)
			index += len(match[0])
		}

		// A root identifier with no literal member path (toJSON(secrets),
		// secrets[expr]) is still a reference to the context, and to all of
		// it; callers decide what an empty path means for their context.
		refs = append(refs, expressionRef{members: members, offset: wordStart + baseOffset})
	}

	return refs
}

// Expression strings escape a quote by doubling it, not with a backslash.
func expressionStringEnd(text string, start int) int {
	quote := text[start]
	for index := start + 1; index < len(text); index++ {
		if text[index] != quote {
			continue
		}

		if index+1 < len(text) && text[index+1] == quote {
			index++

			continue
		}

		return index + 1
	}

	return len(text)
}

func expressionIdent(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '_' || value == '-'
}
