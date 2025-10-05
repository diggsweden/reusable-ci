// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package lexiconguard

import "testing"

// ociTagComponentFragment is the unanchored OCI tag-component charset — the
// value after `:` in a reference, and the rule for a promotion stage name.
// Unlike the anchored digest/hex sentinels, this fragment is DESIGNED to be
// embedded in larger patterns, so it is single-sourced as
// container.OCITagComponent and callers splice that constant in. A verbatim
// re-spelling of the class outside its one home is what this guard forbids.
const ociTagComponentFragment = "[A-Za-z0-9_][A-Za-z0-9._-]{0,127}"

// TestOCITagComponentFragmentIsSingleSourced keeps the tag-component charset
// single-sourced in domain/container (container.OCITagComponent /
// ValidOCITagComponent), the way the commit-SHA, sha256-hex and sha256-digest
// shapes are. The ledger's schema hint patterns and the stage-name validator
// embed the constant, so the literal must not reappear.
func TestOCITagComponentLiteralIsSingleSourced(t *testing.T) {
	t.Parallel()

	singleSource{
		patterns: []string{ociTagComponentFragment},
		owners: []string{
			"internal/lexiconguard/ocitagcomponent_guard_test.go",
			"internal/domain/container/ref.go",
		},
	}.requireSingleSourced(t,
		"OCI tag-component charset re-declared outside domain/container; "+
			"embed container.OCITagComponent (or call ValidOCITagComponent) instead")
}
