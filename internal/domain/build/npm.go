// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build

import (
	"fmt"
	"strings"
	"time"
)

// NPMSummaryInput drives RenderNPMSummary.
type NPMSummaryInput struct {
	PackageName string
	Version     string
	NodeVersion string
	SkipTests   bool
}

// RenderNPMSummary returns the markdown block written by
// scripts/summary/write-npm-build-summary.sh, byte-for-byte.
func RenderNPMSummary(in NPMSummaryInput, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## NPM Build Summary 🔨\n")
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "- **Package:** `%s@%s`\n", in.PackageName, in.Version)
	fmt.Fprintf(&b, "- **Node.js:** %s\n", in.NodeVersion)
	if in.SkipTests {
		fmt.Fprintf(&b, "- **Tests:** ⊘ Skipped\n")
	} else {
		fmt.Fprintf(&b, "- **Tests:** ✓ Executed\n")
	}
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "*Build completed at %s*\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))
	return b.String()
}
