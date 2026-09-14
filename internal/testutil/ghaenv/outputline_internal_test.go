// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package ghaenv

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOutputLine_Boundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name, line, want string
	}{
		{name: "empty"},
		{name: "LF", line: "END\n", want: "END"},
		{name: "CRLF", line: "END\r\n", want: "END"},
		{name: "unterminated", line: "END", want: "END"},
		{name: "terminal CR", line: "END\r", want: "END\r"},
		{name: "only CR", line: "\r", want: "\r"},
		{name: "only LF", line: "\n"},
		{name: "only CRLF", line: "\r\n"},
		{name: "payload CR before CRLF", line: "value\r\r\n", want: "value\r"},
		{name: "embedded newlines", line: "first\r\n\nlast\r\n", want: "first\r\n\nlast"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, outputLine(test.line))
		})
	}
}
