// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"errors"
	"strings"
	"testing"
	"unicode"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
)

func FuzzParseConfigList(f *testing.F) {
	for _, seed := range []string{"", ",,", "p/default", " p/default , custom-rules.yaml, ", "a,b,c", "  ,x,  ,y  ,"} { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		configs, err := security.ParseConfigList(raw)
		if err != nil {
			if !errors.Is(err, errs.ErrUsage) {
				t.Fatalf("ParseConfigList(%q) error is not ErrUsage: %v", raw, err)
			}

			// The only valid failure: raw holds nothing but commas and
			// Unicode whitespace. Tokenisation splits on commas and ASCII
			// whitespace; TrimSpace then drops tokens made of the wider
			// Unicode class (the fuzzer found "\v").
			onlySeparators := strings.IndexFunc(raw, func(r rune) bool {
				return r != ',' && !unicode.IsSpace(r)
			}) == -1
			if !onlySeparators {
				t.Fatalf("ParseConfigList(%q) errored despite non-separator content: %v", raw, err)
			}

			return
		}

		if len(configs) == 0 {
			t.Fatalf("ParseConfigList returned empty slice without error for %q", raw)
		}

		for i, cfg := range configs { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
			if cfg == "" {
				t.Fatalf("configs[%d] empty for %q", i, raw)
			}

			if cfg != strings.TrimSpace(cfg) {
				t.Fatalf("configs[%d] not trimmed: %q", i, cfg)
			}
		}
	})
}
