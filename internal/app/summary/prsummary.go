// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	domainsummary "github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
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
	// At most one lint engine runs (they are mutually exclusive), so the engine
	// that actually executed is the one whose result is not skipped. Label the
	// single Lint row accordingly; default to Nanolinter when none ran.
	lintLabel, lintResult := "Nanolinter", get(pipeline.TargetNanolinter)
	if mega := get(pipeline.TargetMegalinter); lintEngineRan(mega) {
		lintLabel, lintResult = "MegaLinter", mega
	}

	swift := get(pipeline.TargetSwift)

	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "# Pull Request Summary\n\n")
	_, _ = fmt.Fprintf(&b, "## Overview\n")
	_, _ = fmt.Fprintf(&b, "| Property | Value |\n")
	_, _ = fmt.Fprintf(&b, "|----------|-------|\n")
	_, _ = fmt.Fprintf(&b, "| **Project Type** | `%s` |\n", domainsummary.SanitizeCell(in.ProjectType))
	_, _ = fmt.Fprintf(&b, "| **Branch** | `%s` |\n", domainsummary.SanitizeCell(in.Branch))
	_, _ = fmt.Fprintf(&b, "| **Commit** | `%s` |\n", domainsummary.SanitizeCell(short))
	_, _ = fmt.Fprintf(&b, "| **Checked By** | @%s |\n", domainsummary.SanitizeCell(in.Actor))
	_, _ = fmt.Fprintf(&b, "| **Checked At** | %s |\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))
	_, _ = fmt.Fprintf(&b, "\n## Quality Check Status\n")
	_, _ = fmt.Fprintf(&b, "| Check | Status |\n")
	_, _ = fmt.Fprintf(&b, "|-------|--------|\n")
	_, _ = fmt.Fprintf(&b, "| %s | %s |\n", lintLabel, domainsummary.StatusIcon(lintResult))
	_, _ = fmt.Fprintf(&b, "| Swift Lint | %s |\n", domainsummary.StatusIcon(swift))
	_, _ = fmt.Fprintf(&b, "\n## Resources\n")
	_, _ = fmt.Fprintf(&b, "- [Workflow Run](%s)\n", in.RunURL)

	return sink.Append(ctx, b.String())
}

// lintEngineRan reports whether a lint-engine target actually executed (vs.
// being skipped because it was not the selected engine).
func lintEngineRan(result string) bool {
	return result == string(domainsummary.ResultSuccess) || result == string(domainsummary.ResultFailure)
}
