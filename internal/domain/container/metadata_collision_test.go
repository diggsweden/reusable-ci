// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestFormatTags_CollisionsKeepOrderWithoutLosingPriority(t *testing.T) {
	t.Parallel()

	tags := []container.AppliedTag{{Tag: "latest", Priority: 1}, {Tag: "v1", Priority: 5}, {Tag: "latest", Priority: 10}, {Tag: "v1", Priority: 2}}
	require.Equal(t, []string{"registry.example/app:latest", "registry.example/app:v1"}, container.FormatTags("registry.example/app", tags))
	require.Equal(t, "latest", container.PrimaryVersion(tags))
	require.Len(t, tags, 4)
}

// TestFormatTags_RuleDerivedCollisionsKeepTheHighestPriority covers a collision
// produced by the RULES rather than by hand-built duplicates.
//
// The test above constructs AppliedTag values directly with repeated literals,
// which pins FormatTags but says nothing about the shape an adopter actually
// hits: two different rules that legitimately resolve to the same string. A raw
// tag of "v1.2.3" alongside a semver rule on the tag v1.2.3 is the common one,
// and so is a raw "main" beside a ref rule on the main branch. Whichever tag
// wins must be the one with the higher priority, because that is what becomes
// the primary version — the value the release is published and signed under.
func TestFormatTags_RuleDerivedCollisionsKeepTheHighestPriority(t *testing.T) {
	t.Parallel()

	ctx := container.MetadataContext{
		RefName:    "v1.2.3",
		RefType:    provider.RefTypeTag,
		BranchName: "main",
		ShortSHA:   "abc1234",
	}

	rules := []container.Rule{
		// Both resolve to "1.2.3" from different rule types: semver's
		// {{version}} strips the leading v that the ref carries.
		{Type: container.RuleTypeRaw, Enable: true, Value: "1.2.3"},
		{Type: container.RuleTypeSemver, Enable: true, Pattern: "{{version}}"},
		// A third, distinct tag, so deduplication cannot be confused with
		// "only one tag survives".
		{Type: container.RuleTypeSHA, Enable: true, Prefix: "sha-"},
	}

	applied := make([]container.AppliedTag, 0, len(rules))

	for _, rule := range rules {
		tag, ok, err := container.Apply(rule, ctx)
		if err != nil {
			t.Fatalf("Apply(%v): %v", rule.Type, err)
		}

		if !ok {
			t.Fatalf("rule %v produced no tag; the collision below would not occur", rule.Type)
		}

		applied = append(applied, tag)
	}

	// The two colliding rules must genuinely have produced the same tag, or
	// this test is about something else.
	if applied[0].Tag != applied[1].Tag {
		t.Fatalf("the raw and semver rules produced %q and %q; no collision to test",
			applied[0].Tag, applied[1].Tag)
	}

	if applied[0].Priority == applied[1].Priority {
		t.Fatalf("both colliding rules have priority %d; the tie-break is untestable", applied[0].Priority)
	}

	got := container.FormatTags("registry.example/app", applied)
	if len(got) != 2 {
		t.Fatalf("tags = %v, want the collision collapsed to one plus the distinct sha tag", got)
	}

	if got[0] != "registry.example/app:1.2.3" {
		t.Errorf("tags[0] = %q, want the colliding tag emitted once, in declaration order", got[0])
	}

	// The primary version is chosen across the whole applied list, so the
	// collision must not cost the higher-priority rule its claim.
	wantPrimary := applied[0].Tag
	if got := container.PrimaryVersion(applied); got != wantPrimary {
		t.Errorf("PrimaryVersion = %q, want %q", got, wantPrimary)
	}

	// FormatTags must not mutate what it was given: the caller still needs
	// the priorities to compute the primary version afterwards.
	if len(applied) != len(rules) {
		t.Errorf("applied list was modified: %d entries, want %d", len(applied), len(rules))
	}
}
