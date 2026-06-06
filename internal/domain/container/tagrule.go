// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// RuleType is the supported subset of docker/metadata-action tag types.
// Everything outside this set is rejected at parse time.
type RuleType string

// Supported RuleType values.
const (
	RuleTypeRaw    RuleType = "raw"
	RuleTypeRef    RuleType = "ref"
	RuleTypeSemver RuleType = "semver"
	RuleTypeSHA    RuleType = "sha"
)

// RefEvent is the event= attribute on a type=ref rule.
type RefEvent string

// Recognised RefEvent values.
const (
	RefEventBranch RefEvent = "branch"
	RefEventTag    RefEvent = "tag"
	RefEventPR     RefEvent = "pr"
)

// Rule is one parsed tag-rule line. Raw csv attributes the parser doesn't
// recognise are rejected — there's no untyped pass-through.
type Rule struct {
	Type    RuleType
	Enable  bool
	Value   string   // type=raw
	Pattern string   // type=semver: template over {{version}}/{{major}}/{{minor}}/{{patch}} (e.g. v{{major}})
	Event   RefEvent // type=ref
	Prefix  string   // type=sha (may include {{branch}} placeholder)
}

// Priority maps a rule type to the docker/metadata-action default. Used to
// pick the primary version output: highest priority, first declared wins.
func (r Rule) Priority() int { return priorityFor(r.Type) }

func priorityFor(t RuleType) int {
	switch t {
	case RuleTypeSemver:
		return 900
	case RuleTypeRef:
		return 600
	case RuleTypeRaw:
		return 200
	case RuleTypeSHA:
		return 100
	}

	return 0
}

// ParseRules splits TAG_RULES (newline-separated csv lines) into typed
// rules. Blank lines and lines starting with '#' (after trimming) are
// skipped. Any unrecognised type / attribute / pattern / ref event
// returns an error mentioning the offending value.
func ParseRules(input string) ([]Rule, error) {
	var rules []Rule

	for i, raw := range strings.Split(input, "\n") { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		r, err := parseRule(line)
		if err != nil {
			return nil, fmt.Errorf("rule %d: %w", i+1, err)
		}

		rules = append(rules, r)
	}

	return rules, nil
}

//nolint:cyclop // tag-rule parser: one branch per known docker/metadata-action attribute.
func parseRule(line string) (Rule, error) {
	r := Rule{Enable: true} //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	for _, field := range strings.Split(line, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}

		key, val, ok := strings.Cut(field, "=")
		if !ok {
			return r, fmt.Errorf("malformed attribute %q in rule: %s: %w", field, line, errs.ErrValidation)
		}

		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)

		switch key {
		case "type":
			t, err := parseRuleType(val, line)
			if err != nil {
				return r, err
			}

			r.Type = t
		case "enable":
			r.Enable = val == "true"
		case "value":
			r.Value = val
		case "pattern":
			r.Pattern = val
		case "event":
			e, err := parseRefEvent(val, line)
			if err != nil {
				return r, err
			}

			r.Event = e
		case "prefix":
			r.Prefix = val
		case "priority":
			// accepted for compatibility but ignored — defaults are authoritative
		default:
			return r, fmt.Errorf("unsupported attribute %q in rule: %s: %w", key, line, errs.ErrValidation)
		}
	}

	if r.Type == "" {
		return r, fmt.Errorf("rule has no type attribute: %s: %w", line, errs.ErrValidation)
	}

	return r, nil
}

func parseRuleType(val, line string) (RuleType, error) {
	switch val {
	case "raw":
		return RuleTypeRaw, nil
	case "ref":
		return RuleTypeRef, nil
	case "semver":
		return RuleTypeSemver, nil
	case "sha":
		return RuleTypeSHA, nil
	case "pep440", "match", "edge", "schedule":
		return "", fmt.Errorf("tag type %q is not supported by this script: %w", val, errs.ErrUnsupported)
	default:
		return "", fmt.Errorf("unknown tag type %q in rule: %s: %w", val, line, errs.ErrUsage)
	}
}

func parseRefEvent(val, line string) (RefEvent, error) {
	switch val {
	case "branch":
		return RefEventBranch, nil
	case "tag":
		return RefEventTag, nil
	case "pr":
		return RefEventPR, nil
	default:
		return "", fmt.Errorf("unsupported ref event %q in rule: %s: %w", val, line, errs.ErrValidation)
	}
}
