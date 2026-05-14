// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"context"
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	domainsummary "github.com/diggsweden/reusable-ci/internal/domain/summary"
)

// QualityCheck is one row in the quality-check status table.
type QualityCheck struct {
	Name    string
	Enabled bool
	Result  domainsummary.Result
}

// QualityCheckStatus appends the PR-quality-check status block to the
// step summary. Always exits successfully — the underlying job is a
// summary-only collector that surfaces the matrix outcome but never
// fails the pipeline.
//
// Mirrors scripts/summary/write-quality-check-status.sh.
func QualityCheckStatus(ctx context.Context, sink ci.SummarySink, checks []QualityCheck) error {
	var b strings.Builder
	b.WriteString("## Pull Request Check Status\n\n")
	b.WriteString("### Quality Check Results\n")
	b.WriteString("| Check | Status |\n")
	b.WriteString("|-------|--------|\n")
	failed := false
	for _, c := range checks {
		switch {
		case !c.Enabled:
			fmt.Fprintf(&b, "| %s | 🔸 Disabled |\n", c.Name)
		case c.Result == domainsummary.ResultSuccess:
			fmt.Fprintf(&b, "| %s | ✓ Pass |\n", c.Name)
		case c.Result == domainsummary.ResultSkipped:
			fmt.Fprintf(&b, "| %s | − Skipped |\n", c.Name)
		default:
			fmt.Fprintf(&b, "| %s | ✗ Fail |\n", c.Name)
			failed = true
		}
	}
	b.WriteString("\n")
	if failed {
		b.WriteString("### ✗ Some checks failed\n")
		b.WriteString("Please review the failures above and fix any issues.\n")
		b.WriteString("Note: Individual linter failures are shown above. This status job always succeeds to provide summary.\n")
	} else {
		b.WriteString("### ✓ All enabled checks passed\n")
	}
	return sink.Append(ctx, b.String())
}

// ParseQualityChecks splits "Name|enabled|result" lines into checks.
// Mirrors the bash CLI signature.
func ParseQualityChecks(args []string) []QualityCheck {
	out := make([]QualityCheck, 0, len(args))
	for _, raw := range args {
		parts := strings.SplitN(raw, "|", 3)
		if len(parts) < 3 {
			continue
		}
		out = append(out, QualityCheck{
			Name:    parts[0],
			Enabled: parts[1] == "true",
			Result:  domainsummary.NormalizeResult(parts[2]),
		})
	}
	return out
}
