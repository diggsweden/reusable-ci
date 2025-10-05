// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestRunExtractsBoundedProbeFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode string
		body string
		want string
	}{
		{name: "id token", mode: "id-token", body: `{"value":"fixture-token","extra":true}`, want: "fixture-token\n"},
		{name: "Fulcio certificates", mode: "fulcio-certificates", body: `{"chains":[{"certificates":["cert-one","cert-two"]}]}`, want: "cert-one\ncert-two\n"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := run([]string{testCase.mode}, strings.NewReader(testCase.body), &output); err != nil {
				t.Fatal(err)
			}

			if output.String() != testCase.want {
				t.Fatalf("output = %q, want %q", output.String(), testCase.want)
			}
		})
	}
}

func TestRunRejectsMalformedOrAmbiguousProbeJSON(t *testing.T) {
	t.Parallel()

	for _, body := range []string{"", `{}`, `{"value":"token"} {}`} {
		if err := run([]string{"id-token"}, strings.NewReader(body), &bytes.Buffer{}); err == nil {
			t.Errorf("accepted %q", body)
		}
	}
}

func TestRunInputByteLimit(t *testing.T) {
	t.Parallel()

	const limit = 1024 * 1024

	for _, tc := range []struct{ mode, body, want string }{
		{mode: "id-token", body: `{"value":"fixture-token"}`, want: "fixture-token\n"},
		{mode: "fulcio-certificates", body: `{"chains":[{"certificates":["cert-one"]}]}`, want: "cert-one\n"},
	} {
		for _, extra := range []int{0, 1, 17} {
			t.Run(fmt.Sprintf("%s/extra=%d", tc.mode, extra), func(t *testing.T) {
				// A finite input keeps even a removed-limit regression bounded.
				body := tc.body + strings.Repeat(" ", limit-len(tc.body)+extra)
				input := &countingReader{Reader: strings.NewReader(body)}
				output := bytes.NewBufferString("unchanged\n")

				err := run([]string{tc.mode}, input, output)
				if extra == 0 {
					if err != nil || output.String() != "unchanged\n"+tc.want || input.bytes != limit {
						t.Fatalf("at cap: err=%v output=%q read=%d", err, output.String(), input.bytes)
					}

					return
				}

				if !errors.Is(err, errInvalidProbeJSON) || err.Error() != "invalid probe JSON: input must be non-empty and at most 1048576 bytes" {
					t.Fatalf("over cap: got %v, want size refusal", err)
				}

				if output.String() != "unchanged\n" || input.bytes != limit+1 {
					t.Fatalf("over cap: output=%q read=%d, want unchanged output and %d bytes", output.String(), input.bytes, limit+1)
				}
			})
		}
	}
}

type countingReader struct {
	io.Reader
	bytes int
}

func (r *countingReader) Read(body []byte) (int, error) {
	n, err := r.Reader.Read(body)
	r.bytes += n

	return n, err
}
