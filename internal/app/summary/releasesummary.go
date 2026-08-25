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
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	domainsummary "github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
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
	// Image-promotion ladder job results (success/failure/skipped). Empty/
	// skipped for releases without a container.
	PromoteDevResult     string
	PromoteStagingResult string
	PromoteReleaseResult string
	PrepareStageJSON     string
	BuildStageJSON       string
	PublishStageJSON     string
	URLs                 provider.WebURLBuilder // nil when the platform has no web UI
	ServerURL            string                 // CI_SERVER_URL
	Repository           string                 // CI_REPO
	Now                  time.Time
}

// ReleaseSummary appends the release summary block to the step summary.
func ReleaseSummary(ctx context.Context, sink ci.SummarySink, in ReleaseSummaryInput) error {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}

	icon := domainsummary.StatusIcon

	prepare, err := domainsummary.ParseStageResultEnvelope(in.PrepareStageJSON)
	if err != nil {
		return fmt.Errorf("prepare-stage result-json: %w", err)
	}

	build, err := domainsummary.ParseStageResultEnvelope(in.BuildStageJSON)
	if err != nil {
		return fmt.Errorf("build-stage result-json: %w", err)
	}

	publish, err := domainsummary.ParseStageResultEnvelope(in.PublishStageJSON)
	if err != nil {
		return fmt.Errorf("publish-stage result-json: %w", err)
	}

	target := func(stage domainsummary.StageResultEnvelope, key string) string {
		return string(stage.TargetResult(key))
	}

	// reported renders an unset job result as skipped rather than a failure:
	// a promotion job that never ran (a release with no container) reports an
	// empty result here, which is "not applicable", not an error. In a normal
	// run GHA always supplies success/failure/skipped.
	reported := func(result string) string {
		if result == "" {
			return string(domainsummary.ResultSkipped)
		}

		return result
	}

	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "# Release Summary\n\n")
	_, _ = fmt.Fprintf(&b, "## Release Overview\n")
	_, _ = fmt.Fprintf(&b, "| Property | Value |\n")
	_, _ = fmt.Fprintf(&b, "|----------|-------|\n")
	_, _ = fmt.Fprintf(&b, "| **Version** | `%s` |\n", domainsummary.SanitizeCell(in.ReleaseVersion))
	_, _ = fmt.Fprintf(&b, "| **Branch** | `%s` |\n", domainsummary.SanitizeCell(in.ReleaseBranch))
	_, _ = fmt.Fprintf(&b, "| **Commit** | `%s` |\n", domainsummary.SanitizeCell(in.ReleaseCommit))
	_, _ = fmt.Fprintf(&b, "| **Released By** | @%s |\n", domainsummary.SanitizeCell(in.ReleaseActor))
	_, _ = fmt.Fprintf(&b, "| **Released At** | %s |\n\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))
	_, _ = fmt.Fprintf(&b, "## Job Status\n")
	_, _ = fmt.Fprintf(&b, "| Job | Status |\n")
	_, _ = fmt.Fprintf(&b, "|-----|--------|\n")

	rows := []struct {
		label  string
		result string
	}{
		{"Version Bump", target(prepare, pipeline.TargetVersionBump)},
		{"Build Maven", target(build, pipeline.TargetMaven)},
		{"Build NPM", target(build, pipeline.TargetNPM)},
		{"Build Gradle", target(build, pipeline.TargetGradle)},
		{"Build Go", target(build, pipeline.TargetGo)},
		{"Build Cargo", target(build, pipeline.TargetCargo)},
		{"Build Gradle Android", target(build, pipeline.TargetGradleAndroid)},
		{"Build Xcode", target(build, pipeline.TargetXcodeIOS)},
		{"Publish GitHub", target(publish, pipeline.TargetForgePackages)},
		{"Publish Maven Central", target(publish, pipeline.TargetMavenCentral)},
		// The Gradle toolchain publishes through its own pair of jobs (see
		// ArtifactSets.ForgePackagesGradle) — separate rows, not folded into
		// the two above, so a Gradle failure is attributable at a glance.
		{"Publish Forge Packages (Gradle)", target(publish, pipeline.TargetForgePackagesGradle)},
		{"Publish Maven Central (Gradle)", target(publish, pipeline.TargetMavenCentralGradle)},
		{"Publish Apple App Store", target(publish, pipeline.TargetXcodeIOS)},
		{"Publish Google Play", target(publish, pipeline.TargetGooglePlay)},
		{"Containers", target(publish, pipeline.TargetContainers)},
		{"Cargo SBOM", target(publish, pipeline.TargetCargoContainerFirst)},
		{"Go SBOM", target(publish, pipeline.TargetGoContainerFirst)},
		{"Promote Image → dev", reported(in.PromoteDevResult)},
		{"Promote Image → staging", reported(in.PromoteStagingResult)},
		{"Promote Image → release", reported(in.PromoteReleaseResult)},
		{"GitHub Release", in.CreateReleaseResult},
	}
	for _, r := range rows {
		_, _ = fmt.Fprintf(&b, "| %s | %s |\n", r.label, icon(r.result))
	}

	_, _ = fmt.Fprintf(&b, "\n## Resources\n")
	_, _ = fmt.Fprintf(&b, "- [Release](%s)\n",
		domainsummary.ReleaseURL(in.URLs, in.ServerURL, in.Repository, in.ReleaseVersion))
	_, _ = fmt.Fprintf(&b, "- [Packages](%s)\n",
		domainsummary.PackagesURL(in.URLs, in.ServerURL, in.Repository))
	_, _ = fmt.Fprintf(&b, "- [Workflow Run](%s)\n\n", in.RunURL)

	return sink.Append(ctx, b.String())
}
