// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package output_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

func FuzzParseOutputFormat(f *testing.F) {
	for _, seed := range []string{"", "auto", "AUTO", "json", " github ", "yaml", "gitlab", "text", "xml"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, in string) {
		parsed, err := output.Parse(in)
		if err != nil {
			return
		}

		valid := false

		for _, candidate := range output.All() {
			if parsed == candidate {
				valid = true

				break
			}
		}

		if !valid {
			t.Fatalf("Parse returned unknown format %q for %q", parsed, in)
		}

		reparsed, err := output.Parse(string(parsed))
		if err != nil {
			t.Fatalf("reparse of %q failed: %v", parsed, err)
		}

		if reparsed != parsed {
			t.Fatalf("roundtrip changed format: %q -> %q", parsed, reparsed)
		}

		resolved, err := output.ParseAndResolve(in, provider.RunnerGitLab)
		if err != nil {
			t.Fatalf("ParseAndResolve failed after Parse succeeded: %v", err)
		}

		if resolved == output.FormatAuto {
			t.Fatalf("ParseAndResolve returned auto for input %q", in)
		}
	})
}
