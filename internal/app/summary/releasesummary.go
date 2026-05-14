// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	domainsummary "github.com/diggsweden/reusable-ci/internal/domain/summary"
)

// ReleaseSummaryInput drives `summary release`. The three stage JSONs
// are the result-json outputs of summary {prepare,build,publish}-stage-
// result; the use case extracts each target by name.
type ReleaseSummaryInput struct {
	ReleaseVersion      string
	ReleaseBranch       string
	ReleaseCommit       string
	ReleaseActor        string
	RunURL              string
	CreateReleaseResult string
	PrepareStageJSON    string
	BuildStageJSON      string
	PublishStageJSON    string
	Platform            provider.Platform
	ServerURL           string // CI_SERVER_URL
	Repository          string // CI_REPO
	Now                 time.Time
}

// ReleaseSummary appends the release summary block to the step summary.
// Mirrors scripts/summary/write-release-summary.sh.
func ReleaseSummary(ctx context.Context, sink ci.SummarySink, in ReleaseSummaryInput) error {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	icon := domainsummary.StatusIcon

	target := func(stageJSON, key string) string {
		return domainsummary.ExtractTargetResult(stageJSON, key)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Release Summary\n\n")
	fmt.Fprintf(&b, "## Release Overview\n")
	fmt.Fprintf(&b, "| Property | Value |\n")
	fmt.Fprintf(&b, "|----------|-------|\n")
	fmt.Fprintf(&b, "| **Version** | `%s` |\n", in.ReleaseVersion)
	fmt.Fprintf(&b, "| **Branch** | `%s` |\n", in.ReleaseBranch)
	fmt.Fprintf(&b, "| **Commit** | `%s` |\n", in.ReleaseCommit)
	fmt.Fprintf(&b, "| **Released By** | @%s |\n", in.ReleaseActor)
	fmt.Fprintf(&b, "| **Released At** | %s |\n\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintf(&b, "## Job Status\n")
	fmt.Fprintf(&b, "| Job | Status |\n")
	fmt.Fprintf(&b, "|-----|--------|\n")

	rows := []struct {
		label  string
		result string
	}{
		{"Version Bump", target(in.PrepareStageJSON, "version-bump")},
		{"Build Maven", target(in.BuildStageJSON, "maven")},
		{"Build NPM", target(in.BuildStageJSON, "npm")},
		{"Build Gradle", target(in.BuildStageJSON, "gradle")},
		{"Build Gradle Android", target(in.BuildStageJSON, "gradleandroid")},
		{"Build Xcode", target(in.BuildStageJSON, "xcodeios")},
		{"Publish GitHub", target(in.PublishStageJSON, "githubpackages")},
		{"Publish Maven Central", target(in.PublishStageJSON, "mavencentral")},
		{"Publish Apple App Store", target(in.PublishStageJSON, "appleappstore")},
		{"Publish Google Play", target(in.PublishStageJSON, "googleplay")},
		{"Containers", target(in.PublishStageJSON, "containers")},
		{"GitHub Release", in.CreateReleaseResult},
	}
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %s |\n", r.label, icon(r.result))
	}

	fmt.Fprintf(&b, "\n## Resources\n")
	fmt.Fprintf(&b, "- [Release](%s)\n",
		domainsummary.ReleaseURL(in.Platform, in.ServerURL, in.Repository, in.ReleaseVersion))
	fmt.Fprintf(&b, "- [Packages](%s)\n",
		domainsummary.PackagesURL(in.Platform, in.ServerURL, in.Repository))
	fmt.Fprintf(&b, "- [Workflow Run](%s)\n\n", in.RunURL)
	return sink.Append(ctx, b.String())
}
