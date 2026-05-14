// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	domainsummary "github.com/diggsweden/reusable-ci/internal/domain/summary"
)

// PRSummaryInput drives `summary pr`. The QualityStageResultJSON is
// the result-json output of `summary pr-quality-stage-result`; the
// use case extracts each linter's result by name.
type PRSummaryInput struct {
	ProjectType            string
	Branch                 string
	Commit                 string // full SHA — first 7 chars rendered
	Actor                  string
	RunURL                 string
	QualityStageResultJSON string
	// Now is baked in for deterministic testing. Empty → time.Now().
	Now time.Time
}

// PRSummary appends the PR summary block to the step summary.
// Mirrors scripts/summary/write-pr-summary.sh.
func PRSummary(ctx context.Context, sink ci.SummarySink, in PRSummaryInput) error {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	short := in.Commit
	if len(short) > 7 {
		short = short[:7]
	}

	get := func(key string) string {
		return domainsummary.ExtractTargetResult(in.QualityStageResultJSON, key)
	}
	dep := get("dependencyreview")
	sast := get("sastopengrep")
	publiccode := get("publiccodelint")
	devbase := get("devbasecheck")
	swift := get("swift")

	var b strings.Builder
	fmt.Fprintf(&b, "# Pull Request Summary\n\n")
	fmt.Fprintf(&b, "## Overview\n")
	fmt.Fprintf(&b, "| Property | Value |\n")
	fmt.Fprintf(&b, "|----------|-------|\n")
	fmt.Fprintf(&b, "| **Project Type** | `%s` |\n", in.ProjectType)
	fmt.Fprintf(&b, "| **Branch** | `%s` |\n", in.Branch)
	fmt.Fprintf(&b, "| **Commit** | `%s` |\n", short)
	fmt.Fprintf(&b, "| **Checked By** | @%s |\n", in.Actor)
	fmt.Fprintf(&b, "| **Checked At** | %s |\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintf(&b, "\n## Quality Check Status\n")
	fmt.Fprintf(&b, "| Check | Status |\n")
	fmt.Fprintf(&b, "|-------|--------|\n")
	fmt.Fprintf(&b, "| Devbase Check | %s |\n", domainsummary.StatusIcon(devbase))
	fmt.Fprintf(&b, "| Dependency Review | %s |\n", domainsummary.StatusIcon(dep))
	fmt.Fprintf(&b, "| OpenGrep SAST | %s |\n", domainsummary.StatusIcon(sast))
	fmt.Fprintf(&b, "| Publiccode Lint | %s |\n", domainsummary.StatusIcon(publiccode))
	fmt.Fprintf(&b, "| Swift Lint | %s |\n", domainsummary.StatusIcon(swift))
	fmt.Fprintf(&b, "\n## Resources\n")
	fmt.Fprintf(&b, "- [Workflow Run](%s)\n", in.RunURL)
	return sink.Append(ctx, b.String())
}
