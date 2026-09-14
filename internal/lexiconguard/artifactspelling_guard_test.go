// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package lexiconguard keeps a spelling, a regex, or a pattern literal
// declared exactly once in the tree.
//
// Each guard names one canonical definition, then walks the repository for
// anything that re-states it. They clear the bar ADR 0003 §3 sets: each pins a
// fact with one correct value rather than a judgement, and a second copy is
// expensive to reverse because the two drift apart silently. A digest regex
// that accepts one more character in one place than another is a validation
// hole, not a style problem.
//
// One of the repo-wide guard packages; docs/testing.md says which is which and
// where a new guard belongs.
package lexiconguard

import "testing"

// britishArtifact is the British spelling the codebase standardized away from.
// The domain, identifiers, CLI usage strings, and prose all use the American
// "artifact"; the wire contract (artifacts.yml, --artifact, JSON keys) was
// already American, so this guard keeps the Go source from drifting back to a
// mixed spelling.
const britishArtifact = "artefact"

// TestArtifactSpellingIsAmerican fails if any Go source reintroduces the
// British "artefact"/"Artefact" spelling, the way the commit-SHA and
// retry-backoff guards lock their single source.
func TestArtifactSpellingIsAmerican(t *testing.T) {
	t.Parallel()

	singleSource{
		patterns: []string{britishArtifact},
		owners:   []string{"internal/lexiconguard/artifactspelling_guard_test.go"},
		// A spelling rule, not a pattern literal: "Artefact" offends too.
		fold: true,
	}.requireSingleSourced(t,
		`British "artefact" spelling found; use "artifact"`)
}
