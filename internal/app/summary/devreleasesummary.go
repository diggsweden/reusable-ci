// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	domainsummary "github.com/diggsweden/reusable-ci/internal/domain/summary"
)

// DevReleaseSummaryInput drives `summary dev-release`. PublishStageJSON
// supplies container/npm results; DevArtifactsJSON has npm package
// metadata + a publish-status sentinel ("already-exists" → "skipped"
// rendering with a clarifying note).
type DevReleaseSummaryInput struct {
	ProjectType       projecttype.Type
	ReleaseRef        string
	ReleaseSHA        string
	ReleaseActor      string
	ReleaseRepository string
	RunURL            string
	PublishStageJSON  string
	DevArtifactsJSON  string
	Platform          provider.Platform
	ServerURL         string
	Now               time.Time
}

// DevReleaseSummary appends the dev-release block to the step summary
// and prints a short stdout banner the bash also produces.
//
// Mirrors scripts/summary/write-dev-release-summary.sh.
func DevReleaseSummary(ctx context.Context, sink ci.SummarySink, stdout io.Writer, in DevReleaseSummaryInput) error {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	short := in.ReleaseSHA
	if len(short) > 7 {
		short = short[:7]
	}
	containerStatus := domainsummary.ExtractTargetResult(in.PublishStageJSON, "container")
	npmStatus := domainsummary.ExtractTargetResult(in.PublishStageJSON, "npm")
	npmPackageName := domainsummary.ExtractTargetResult(in.DevArtifactsJSON, "npm_package_name")
	npmPackageVersion := domainsummary.ExtractTargetResult(in.DevArtifactsJSON, "npm_package_version")
	npmPublishStatus := domainsummary.ExtractTargetResult(in.DevArtifactsJSON, "npm_publish_status")
	// ExtractTargetResult returns "skipped" sentinel for missing/empty
	// values; the bash treats those fields as absent strings. Normalise.
	if npmPackageName == "skipped" {
		npmPackageName = ""
	}
	if npmPackageVersion == "skipped" {
		npmPackageVersion = ""
	}
	if npmPublishStatus == "skipped" {
		npmPublishStatus = ""
	}

	fmt.Fprintf(stdout, "================================================\n")
	fmt.Fprintf(stdout, "Generating Dev Release Summary\n")
	fmt.Fprintf(stdout, "================================================\n")
	fmt.Fprintf(stdout, "Project Type: %s\n", in.ProjectType)
	fmt.Fprintf(stdout, "Branch: %s\n", in.ReleaseRef)
	fmt.Fprintf(stdout, "Commit: %s\n", short)
	pkgLabel := npmPackageName
	if pkgLabel == "" {
		pkgLabel = "none"
	}
	verLabel := npmPackageVersion
	if verLabel == "" {
		verLabel = "none"
	}
	fmt.Fprintf(stdout, "NPM Package: %s@%s\n\n", pkgLabel, verLabel)

	var b strings.Builder
	fmt.Fprintf(&b, "# Dev Release Summary\n\n")
	fmt.Fprintf(&b, "## Build Information\n")
	fmt.Fprintf(&b, "| Property | Value |\n")
	fmt.Fprintf(&b, "|----------|-------|\n")
	fmt.Fprintf(&b, "| **Project Type** | `%s` |\n", in.ProjectType)
	fmt.Fprintf(&b, "| **Branch** | `%s` |\n", in.ReleaseRef)
	fmt.Fprintf(&b, "| **Commit** | `%s` |\n", short)
	fmt.Fprintf(&b, "| **Built By** | @%s |\n", in.ReleaseActor)
	fmt.Fprintf(&b, "| **Built At** | %s |\n\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintf(&b, "## Job Status\n")
	fmt.Fprintf(&b, "| Job | Status |\n")
	fmt.Fprintf(&b, "|-----|--------|\n")
	fmt.Fprintf(&b, "| Build Container | %s |\n", domainsummary.StatusIcon(containerStatus))
	if in.ProjectType == projecttype.NPM {
		row := fmt.Sprintf("| Publish NPM Package | %s |\n", domainsummary.StatusIcon(npmStatus))
		if npmPublishStatus == "already-exists" {
			row = fmt.Sprintf("| Publish NPM Package | %s (already published — skipped) |\n",
				domainsummary.StatusIcon(npmStatus))
		}
		b.WriteString(row)
	}

	fmt.Fprintf(&b, "\n## Published Artifacts\n")
	if in.ProjectType == projecttype.NPM {
		if npmPackageName != "" && npmPackageVersion != "" && npmStatus == "success" {
			fmt.Fprintf(&b, "\n### NPM Package\n")
			if npmPublishStatus == "already-exists" {
				fmt.Fprintf(&b, "> **Note:** Version already existed in registry — publish was skipped (same commit SHA).\n\n")
			}
			fmt.Fprintf(&b, "```\n%s@%s\n```\n\n", npmPackageName, npmPackageVersion)
			fmt.Fprintf(&b, "```bash\nnpm install %s@%s\nnpm install %s@dev\n```\n",
				npmPackageName, npmPackageVersion, npmPackageName)
		} else {
			fmt.Fprintf(&b, "\n### NPM Package\nNot published\n")
		}
	}

	fmt.Fprintf(&b, "\n## Resources\n")
	fmt.Fprintf(&b, "- [Packages](%s)\n",
		domainsummary.PackagesURL(in.Platform, in.ServerURL, in.ReleaseRepository))
	fmt.Fprintf(&b, "- [Workflow Run](%s)\n\n", in.RunURL)
	fmt.Fprintf(&b, "These are development artifacts tagged with `dev` and are not intended for production use.\n")

	if err := sink.Append(ctx, b.String()); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "✓ Dev release summary generated successfully\n")
	return nil
}
