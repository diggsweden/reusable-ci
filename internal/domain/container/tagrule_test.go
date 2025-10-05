// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
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
	// A type nobody has heard of is a typo in the operator's tag rules.
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	if !strings.Contains(err.Error(), `unknown tag type "banana"`) {
		t.Errorf("error = %v", err)
	}
}

func TestParseRules_RefusesActionTypesNotImplemented(t *testing.T) {
	t.Parallel()

	// These are real docker/metadata-action types this tool has not
	// implemented, so they classify as ErrUnsupported (78) rather than as the
	// ErrUsage a misspelling gets -- the operator's rule is valid upstream.
	for _, typ := range []string{"pep440", "match", "edge", "schedule"} {
		_, err := container.ParseRules("type=" + typ + ",enable=true")
		if !errors.Is(err, errs.ErrUnsupported) {
			t.Errorf("type=%s: err = %v, want ErrUnsupported", typ, err)

			continue
		}

		if !strings.Contains(err.Error(), "is not supported by this script") {
			t.Errorf("type=%s error wrong: %v", typ, err)
		}
	}
}

func TestParseRules_MissingType(t *testing.T) {
	t.Parallel()

	_, err := container.ParseRules("value=oops,enable=true")
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if !strings.Contains(err.Error(), "no type attribute") {
		t.Errorf("error = %v", err)
	}
}

func TestParseRules_UnsupportedAttribute(t *testing.T) {
	t.Parallel()

	_, err := container.ParseRules("type=raw,unknown=xx")
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if !strings.Contains(err.Error(), `unsupported attribute "unknown"`) {
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
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if !strings.Contains(err.Error(), `unsupported ref event "stable"`) {
		t.Errorf("error = %v", err)
	}
}

// TestParseRules_EnableTakesOnlyTrueOrFalse pins the enable grammar.
//
// Any value other than "true" used to disable the rule, so "enable=TRUE",
// "enable=1" or "enable=yes" dropped a tag from the release with no error --
// the kind of mistake that is only noticed when a consumer pulls a tag that
// was never pushed. The workflows render enable from boolean expressions,
// which produce exactly "true" or "false", so refusing everything else costs
// no supported input.
func TestParseRules_EnableTakesOnlyTrueOrFalse(t *testing.T) {
	t.Parallel()

	for value, want := range map[string]bool{"true": true, "false": false} {
		rules, err := container.ParseRules("type=raw,value=stable,enable=" + value)
		if err != nil || len(rules) != 1 || rules[0].Enable != want {
			t.Errorf("enable=%s: rules = %+v, err = %v, want Enable=%v", value, rules, err, want)
		}
	}

	for _, value := range []string{"TRUE", "True", "1", "yes", "", "on"} {
		if rules, err := container.ParseRules("type=raw,value=stable,enable=" + value); !errors.Is(err, errs.ErrValidation) {
			t.Errorf("enable=%q: rules = %+v, err = %v, want ErrValidation", value, rules, err)
		}
	}
}

// TestParseRules_RefusesARepeatedAttribute covers the other silent resolution:
// a repeated attribute took its last value, so "type=raw,type=sha" became a
// sha rule and "value=a,value=b" tagged b. Neither has a reading the author
// can be assumed to have meant.
func TestParseRules_RefusesARepeatedAttribute(t *testing.T) {
	t.Parallel()

	for _, line := range []string{
		"type=raw,type=sha",
		"type=raw,value=a,value=b",
		"type=sha,prefix=a-,prefix=b-",
		"type=raw,value=a,enable=true,enable=false",
	} {
		_, err := container.ParseRules(line)
		if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "more than once") {
			t.Errorf("%q: err = %v, want ErrValidation naming the repeat", line, err)
		}
	}

	// Empty fields are not attributes, so trailing and doubled commas stay
	// harmless.
	rules, err := container.ParseRules("type=raw,,value=stable,")
	if err != nil || len(rules) != 1 || rules[0].Value != "stable" {
		t.Errorf("empty fields: rules = %+v, err = %v, want one raw rule", rules, err)
	}
}

// TestParseRules_ALateInvalidRuleIsNumberedByItsInputLine covers a failure
// after valid rules. The parse must refuse the whole list rather than return
// the rules before the bad one, and the number in the error must be the line
// the operator sees in their workflow, counting the comments and blanks that
// were skipped.
func TestParseRules_ALateInvalidRuleIsNumberedByItsInputLine(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		"type=raw,value=stable",
		"# a comment",
		"",
		"type=sha,prefix=sha-",
		"type=banana",
	}, "\n")

	rules, err := container.ParseRules(input)
	if err == nil {
		t.Fatalf("a late invalid rule was accepted: %+v", rules)
	}

	if rules != nil {
		t.Errorf("a refused list still returned %d rule(s)", len(rules))
	}

	if !strings.HasPrefix(err.Error(), "rule 5: ") {
		t.Errorf("err = %v, want it numbered by input line 5", err)
	}
}
