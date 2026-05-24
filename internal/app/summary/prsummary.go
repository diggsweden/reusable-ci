// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/pipeline"
	domainsummary "github.com/diggsweden/reusable-ci/internal/domain/summary"
)

// PRSummaryInput drives `summary pr`. The QualityStageResultJSON is the
// quality stage result-json output; the use case extracts each linter's result
// by name.
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
func PRSummary(ctx context.Context, sink ci.SummarySink, in PRSummaryInput) error {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}

	short := in.Commit
	if len(short) > 7 {
		short = short[:7]
	}

	quality, err := domainsummary.ParseStageResultEnvelope(in.QualityStageResultJSON)
	if err != nil {
		return fmt.Errorf("quality-stage result-json: %w", err)
	}

	get := func(key string) string {
		return string(quality.TargetResult(key))
	}
	dep := get(pipeline.TargetDependencyReview)
	sast := get(pipeline.TargetSASTOpengrep)
	publiccode := get(pipeline.TargetPublicCodeLint)
	devbase := get(pipeline.TargetDevbaseCheck)
	swift := get(pipeline.TargetSwift)

	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "# Pull Request Summary\n\n")
	_, _ = fmt.Fprintf(&b, "## Overview\n")
	_, _ = fmt.Fprintf(&b, "| Property | Value |\n")
	_, _ = fmt.Fprintf(&b, "|----------|-------|\n")
	_, _ = fmt.Fprintf(&b, "| **Project Type** | `%s` |\n", in.ProjectType)
	_, _ = fmt.Fprintf(&b, "| **Branch** | `%s` |\n", in.Branch)
	_, _ = fmt.Fprintf(&b, "| **Commit** | `%s` |\n", short)
	_, _ = fmt.Fprintf(&b, "| **Checked By** | @%s |\n", in.Actor)
	_, _ = fmt.Fprintf(&b, "| **Checked At** | %s |\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))
	_, _ = fmt.Fprintf(&b, "\n## Quality Check Status\n")
	_, _ = fmt.Fprintf(&b, "| Check | Status |\n")
	_, _ = fmt.Fprintf(&b, "|-------|--------|\n")
	_, _ = fmt.Fprintf(&b, "| Devbase Check | %s |\n", domainsummary.StatusIcon(devbase))
	_, _ = fmt.Fprintf(&b, "| Dependency Review | %s |\n", domainsummary.StatusIcon(dep))
	_, _ = fmt.Fprintf(&b, "| OpenGrep SAST | %s |\n", domainsummary.StatusIcon(sast))
	_, _ = fmt.Fprintf(&b, "| Publiccode Lint | %s |\n", domainsummary.StatusIcon(publiccode))
	_, _ = fmt.Fprintf(&b, "| Swift Lint | %s |\n", domainsummary.StatusIcon(swift))
	_, _ = fmt.Fprintf(&b, "\n## Resources\n")
	_, _ = fmt.Fprintf(&b, "- [Workflow Run](%s)\n", in.RunURL)

	return sink.Append(ctx, b.String())
}
