// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/container"
)

func TestParseRules_DefaultEnable(t *testing.T) {
	t.Parallel()
	rules, err := container.ParseRules("type=raw,value=stable")
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("len(rules) = %d", len(rules))
	}
	if !rules[0].Enable {
		t.Error("Enable should default to true")
	}
}

func TestParseRules_BlankLinesAndCommentsSkipped(t *testing.T) {
	t.Parallel()
	input := "\n# header comment\ntype=raw,value=keep,enable=true\n\n  # indented comment ignored\n"
	rules, err := container.ParseRules(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Value != "keep" {
		t.Errorf("rules = %+v", rules)
	}
}

func TestParseRules_AllSupportedTypes(t *testing.T) {
	t.Parallel()
	input := strings.Join([]string{
		"type=raw,value=v1",
		"type=ref,event=branch",
		"type=ref,event=tag",
		"type=ref,event=pr",
		"type=semver,pattern={{version}}",
		"type=sha,prefix=sha-",
	}, "\n")
	rules, err := container.ParseRules(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 6 {
		t.Fatalf("got %d rules", len(rules))
	}
	wantTypes := []container.RuleType{
		container.RuleTypeRaw, container.RuleTypeRef, container.RuleTypeRef,
		container.RuleTypeRef, container.RuleTypeSemver, container.RuleTypeSHA,
	}
	for i, r := range rules {
		if r.Type != wantTypes[i] {
			t.Errorf("rules[%d].Type = %q, want %q", i, r.Type, wantTypes[i])
		}
	}
}

func TestParseRules_UnknownType(t *testing.T) {
	t.Parallel()
	_, err := container.ParseRules("type=banana,enable=true")
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
	if !strings.Contains(err.Error(), `unknown tag type "banana"`) {
		t.Errorf("error = %v", err)
	}
}

func TestParseRules_RefusesActionTypesNotImplemented(t *testing.T) {
	t.Parallel()
	for _, typ := range []string{"pep440", "match", "edge", "schedule"} {
		_, err := container.ParseRules("type=" + typ + ",enable=true")
		if err == nil {
			t.Errorf("type=%s should error", typ)
		}
		if err != nil && !strings.Contains(err.Error(), "is not supported by this script") {
			t.Errorf("type=%s error wrong: %v", typ, err)
		}
	}
}

func TestParseRules_MissingType(t *testing.T) {
	t.Parallel()
	_, err := container.ParseRules("value=oops,enable=true")
	if err == nil || !strings.Contains(err.Error(), "no type attribute") {
		t.Errorf("error = %v", err)
	}
}

func TestParseRules_UnsupportedAttribute(t *testing.T) {
	t.Parallel()
	_, err := container.ParseRules("type=raw,unknown=xx")
	if err == nil || !strings.Contains(err.Error(), `unsupported attribute "unknown"`) {
		t.Errorf("error = %v", err)
	}
}

func TestParseRules_PriorityIgnored(t *testing.T) {
	t.Parallel()
	// priority= is accepted but doesn't override the default tier.
	rules, err := container.ParseRules("type=sha,prefix=sha-,priority=999")
	if err != nil {
		t.Fatal(err)
	}
	if rules[0].Priority() != 100 {
		t.Errorf("Priority = %d, want 100 (sha default, ignoring priority=)", rules[0].Priority())
	}
}

func TestRule_PriorityTiers(t *testing.T) {
	t.Parallel()
	cases := map[container.RuleType]int{
		container.RuleTypeSemver: 900,
		container.RuleTypeRef:    600,
		container.RuleTypeRaw:    200,
		container.RuleTypeSHA:    100,
	}
	for typ, want := range cases {
		r := container.Rule{Type: typ}
		if got := r.Priority(); got != want {
			t.Errorf("Priority(%q) = %d, want %d", typ, got, want)
		}
	}
}

func TestParseRules_RefBadEvent(t *testing.T) {
	t.Parallel()
	_, err := container.ParseRules("type=ref,event=stable")
	if err == nil || !strings.Contains(err.Error(), `unsupported ref event "stable"`) {
		t.Errorf("error = %v", err)
	}
}
