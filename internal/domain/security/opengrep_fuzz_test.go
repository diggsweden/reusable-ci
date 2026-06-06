// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/security"
)

func FuzzParseConfigList(f *testing.F) {
	for _, seed := range []string{"", ",,", "p/default", " p/default , custom-rules.yaml, ", "a,b,c", "  ,x,  ,y  ,"} { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		configs, err := security.ParseConfigList(raw)
		if err != nil {
			// Only valid failure shape: every comma-split entry trims to empty.
			for _, part := range strings.Split(raw, ",") {
				if strings.TrimSpace(part) != "" {
					return
				}
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
