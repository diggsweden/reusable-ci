// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
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
		"type=raw,value=a\xffb",
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

			// A value is one CSV field of one line, trimmed. That is the
			// parser's actual contract. It used to assert valid UTF-8 here,
			// which the parser never promised: it keeps bytes, and Apply
			// refuses a tag that is not valid under the OCI grammar -- a seed
			// containing 0xFF failed this property while Apply correctly
			// rejected the resulting tag. The byte-level check belongs to Apply
			// and is pinned there.
			for _, value := range []string{rule.Value, rule.Pattern, rule.Prefix} {
				if strings.ContainsAny(value, ",\n") || value != strings.TrimSpace(value) {
					t.Fatalf("rules[%d] has a value that is not one trimmed field: %q", i, value)
				}
			}
		}
	})
}
