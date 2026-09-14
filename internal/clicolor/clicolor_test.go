// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package clicolor_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
)

// The non-terminal rule is asserted in policy_internal_test.go rather than
// here. A black-box test cannot reach the process-wide colour policy, which is
// initialised from the ambient NO_COLOR / TERM before any test runs, so the
// version that used to live in this file passed without exercising the
// terminal check at all on any machine that sets either — measured: with
// isTerminal forced to true it failed on a plain host and passed under
// NO_COLOR=1, while the internal test caught the same mutation in both.

// TestGlyphConstants_PinTheSuccessAndFailureMarks guards the CLI-side glyphs.
//
// domain/summary hardcodes the same two marks in StatusIcon rather than
// importing these, and that duplication is required, not an oversight: ADR 0004
// layering says every arrow points inward at domain, so domain cannot import a
// CLI package. Each side therefore needs its own pin, and merging them would
// break the layering guard. If these ever drift apart, the CLI and the step
// summary disagree about what a passing check looks like.
func TestGlyphConstants_PinTheSuccessAndFailureMarks(t *testing.T) {
	t.Parallel()

	if clicolor.Success != "✓" || clicolor.Failure != "✗" {
		t.Errorf("glyph constants drifted: %q / %q", clicolor.Success, clicolor.Failure)
	}
}
