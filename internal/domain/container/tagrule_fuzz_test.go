// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"testing"
	"unicode/utf8"

	"github.com/diggsweden/reusable-ci/internal/domain/container"
)

//nolint:cyclop // fuzz harness exercising the parser exhaustively.
func FuzzParseRules(f *testing.F) {
	seeds := []string{
		"",
		"# comment only",
		"type=raw,value=stable",
		"type=ref,event=branch",
		"type=ref,event=tag",
		"type=ref,event=pr",
		"type=semver,pattern={{version}}",
		"type=sha,prefix=sha-",
		"type=raw,value=stable,enable=false",
		"type=sha,prefix={{branch}}-",
		"type=banana,enable=true",
		"type=raw,unknown=xx",
		"value=oops,enable=true",
		"type=ref,event=stable",
		"type=raw,value=a\ntype=semver,pattern={{major}}.{{minor}}",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		rules, err := container.ParseRules(input)
		if err != nil {
			return
		}

		for i, rule := range rules { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
			if rule.Type == "" {
				t.Fatalf("rules[%d] has empty type for input %q", i, input)
			}

			switch rule.Type {
			case container.RuleTypeRaw, container.RuleTypeRef, container.RuleTypeSemver, container.RuleTypeSHA:
			default:
				t.Fatalf("rules[%d] has unsupported type %q for input %q", i, rule.Type, input)
			}

			if rule.Priority() <= 0 {
				t.Fatalf("rules[%d] has non-positive priority %d", i, rule.Priority())
			}

			if rule.Event != "" {
				switch rule.Event {
				case container.RefEventBranch, container.RefEventTag, container.RefEventPR:
				default:
					t.Fatalf("rules[%d] has invalid event %q", i, rule.Event)
				}
			}

			for _, value := range []string{rule.Value, rule.Pattern, rule.Prefix} {
				if value != "" && !utf8.ValidString(value) {
					t.Fatalf("rule contains invalid UTF-8 string %q", value)
				}
			}
		}
	})
}
