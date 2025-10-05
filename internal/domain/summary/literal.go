// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary

import (
	"strconv"
	"strings"
	"unicode"
)

// LiteralText encodes data for inline heading, prose and table values, not
// Markdown source, link destinations, HTML attributes or fenced code blocks.
// Each control becomes one space; other Unicode and ordinary spaces are kept.
// Entities prevent Markdown/HTML interpretation without changing the text.
func LiteralText(value string) string {
	var out strings.Builder
	out.Grow(len(value))

	for index, char := range value {
		switch {
		case unicode.IsControl(char):
			out.WriteByte(' ')
		case strings.ContainsRune("&<>\"'\\`|[]*_~#!:@", char) ||
			(char == '.' && index >= 3 && strings.EqualFold(value[index-3:index], "www")):
			// Also break bare URL/email autolinks, including the www. form.
			out.WriteString("&#")
			out.WriteString(strconv.Itoa(int(char)))
			out.WriteByte(';')
		default:
			out.WriteRune(char)
		}
	}

	return out.String()
}

// InlineCode returns a complete inline code value, also safe in a table cell.
// Simple values retain Markdown backticks. Empty values, boundary spaces,
// backticks and table delimiters use HTML code with encoded inline content:
// raw <code> tags alone do not suppress Markdown parsing inside them.
func InlineCode(value string) string {
	value = strings.Map(func(char rune) rune {
		if unicode.IsControl(char) {
			return ' '
		}

		return char
	}, value)
	if value == "" || strings.ContainsAny(value, "`|") || strings.HasPrefix(value, " ") || strings.HasSuffix(value, " ") {
		return "<code>" + LiteralText(value) + "</code>"
	}

	return "`" + value + "`"
}
