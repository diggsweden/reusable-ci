// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPanicArgs_RedactsBothTokenSpellings(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"reusable-ci", "platform", "checkout", "--token", "synthetic-secret", "--repository", "owner/repo"}, {"reusable-ci", "release", "verify-release-tag", "--token=synthetic-secret"}} {
		original := append([]string(nil), args...)

		var out bytes.Buffer
		formatPanic(&out, "fixture failure", []byte("stack"), "version", "commit", args)
		require.NotContains(t, out.String(), "synthetic-secret")
		require.Contains(t, out.String(), "redacted")

		link, err := url.Parse(bugReportLink("failure", "version", "commit", args))
		require.NoError(t, err)
		require.NotContains(t, link.Query().Get("body"), "synthetic-secret")
		require.Equal(t, original, args)
	}
}

func TestFormatPanic_OmitsSensitiveInputsAndNonerrorPayloads(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, kind string
		payload    any
	}{
		{"string", "string", "fixture-payload-secret"},
		{"integer", "int", 93827461},
		{"anonymous struct", "struct", struct {
			Secret string `credential:"fixture-tag-secret"`
		}{"fixture-payload-secret"}},
		{"map", "map", map[string]string{"fixture-map-secret": "fixture-payload-secret"}},
		{"synthetic file contents", "slice", []byte("fixture-file-content-secret")},
		{"error", "struct", sentinelError{msg: "fixture-payload-secret"}},
		{"formatter", "struct", panicFormatter{secret: "fixture-payload-secret"}},
		{"nil", "invalid", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"fixture-executable-secret", "--token", "fixture-token-secret", "--password=fixture-password-secret", "https://user:fixture-url-secret@example.invalid", "--private-key-file", "fixture-path-secret", "--", "fixture-positional-secret"}
			original := append([]string(nil), args...)

			var out bytes.Buffer
			formatPanic(&out, tc.payload, []byte("trusted stack frame"), "v1", "sha1", args)
			require.Equal(t, original, args)
			require.Equal(t, "reusable-ci: internal error: "+tc.kind+" value [redacted]", strings.SplitN(out.String(), "\n", 2)[0])
			require.Contains(t, out.String(), "trusted stack frame")
			require.Contains(t, out.String(), "version=v1  commit=sha1")
			decoded, err := url.QueryUnescape(out.String())
			require.NoError(t, err)

			for _, secret := range append(original, "fixture-password-secret", "fixture-url-secret", "fixture-payload-secret", "fixture-tag-secret", "fixture-map-secret", "fixture-file-content-secret", "93827461", "formatter must not run") {
				require.NotContains(t, decoded, secret)
			}

			require.NotContains(t, decoded, "%!")
		})
	}
}

func TestFormatPanic_DoesNotInvokePayloadMethods(t *testing.T) {
	t.Parallel()

	var errorCalls, stringCalls, formatCalls int
	for _, tc := range []struct {
		name    string
		payload any
		calls   *int
	}{
		{"Error", countedPanicError{&errorCalls}, &errorCalls},
		{"String", countedPanicStringer{&stringCalls}, &stringCalls},
		{"Format", countedPanicFormatter{&formatCalls}, &formatCalls},
	} {
		t.Run(tc.name, func(t *testing.T) {
			formatPanic(io.Discard, tc.payload, []byte("stack"), "v", "c", nil)
			require.Zero(t, *tc.calls, "reporting must not invoke the payload, even if its output is discarded")
			// Each fixture implements only one formatting interface. This control
			// proves it is active rather than hidden by fmt's method precedence.
			require.Equal(t, "synthetic callback result", fmt.Sprintf("%v", tc.payload))
			require.Equal(t, 1, *tc.calls)
		})
	}
}

type countedPanicError struct{ calls *int }

func (p countedPanicError) Error() string {
	*p.calls += 1

	return "synthetic callback result"
}

type countedPanicStringer struct{ calls *int }

func (p countedPanicStringer) String() string {
	*p.calls += 1

	return "synthetic callback result"
}

type countedPanicFormatter struct{ calls *int }

func (p countedPanicFormatter) Format(state fmt.State, _ rune) {
	*p.calls += 1
	_, _ = fmt.Fprint(state, "synthetic callback result")
}

// A reporting path must not invoke arbitrary payload code, even to redact its
// result. These methods would panic again and could themselves leak secrets.
type panicFormatter struct{ secret string }

func (p panicFormatter) String() string { panic("formatter must not run: " + p.secret) }

func (p panicFormatter) Format(fmt.State, rune) { panic("formatter must not run: " + p.secret) }
