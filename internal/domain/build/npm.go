// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

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

// RenderNPMSummary returns the markdown block written by.
func RenderNPMSummary(in NPMSummaryInput, now time.Time) string {
	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "## NPM Build Summary 🔨\n")
	_, _ = fmt.Fprintf(&b, "\n")
	_, _ = fmt.Fprintf(&b, "- **Package:** `%s@%s`\n", in.PackageName, in.Version)
	_, _ = fmt.Fprintf(&b, "- **Node.js:** %s\n", in.NodeVersion)

	if in.SkipTests {
		_, _ = fmt.Fprintf(&b, "- **Tests:** ⊘ Skipped\n")
	} else {
		_, _ = fmt.Fprintf(&b, "- **Tests:** ✓ Executed\n")
	}

	_, _ = fmt.Fprintf(&b, "\n")
	_, _ = fmt.Fprintf(&b, "*Build completed at %s*\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))

	return b.String()
}
