// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	domainsummary "github.com/diggsweden/reusable-ci/internal/domain/summary"
)

// DevReleaseSummaryInput drives `summary dev-release`. BuildStageJSON and
// PublishStageJSON supply job results; DevArtifactsJSON has npm package
// metadata + a publish-status sentinel ("already-exists" → "skipped"
// rendering with a clarifying note).
type DevReleaseSummaryInput struct {
	ProjectType       projecttype.Type
	ReleaseRef        string
	ReleaseSHA        string
	ReleaseActor      string
	ReleaseRepository string
	RunURL            string
	BuildStageJSON    string
	PublishStageJSON  string
	DevArtifactsJSON  string
	Platform          provider.Platform
	ServerURL         string
	Now               time.Time
}

// DevReleaseSummary appends the dev-release block to the step summary and
// prints a short w banner.
//
//nolint:cyclop // renders one summary block per dev-release artifact category.
func DevReleaseSummary(ctx context.Context, sink ci.SummarySink, w io.Writer, in DevReleaseSummaryInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}

	short := in.ReleaseSHA
	if len(short) > 7 {
		short = short[:7]
	}

	build, err := domainsummary.ParseStageResultEnvelope(in.BuildStageJSON)
	if err != nil {
		return fmt.Errorf("dev-build-stage result-json: %w", err)
	}

	publish, err := domainsummary.ParseStageResultEnvelope(in.PublishStageJSON)
	if err != nil {
		return fmt.Errorf("dev-publish-stage result-json: %w", err)
	}

	target := func(stage domainsummary.StageResultEnvelope, key string) string {
		return string(stage.TargetResult(key))
	}
	containerStatus := target(publish, pipeline.TargetContainers)
	npmStatus := target(publish, pipeline.TargetNPM)
	buildMavenStatus := target(build, pipeline.TargetMaven)
	buildNPMStatus := target(build, pipeline.TargetNPM)
	buildGradleStatus := target(build, pipeline.TargetGradle)
	buildGoStatus := target(build, pipeline.TargetGo)
	buildCargoStatus := target(build, pipeline.TargetCargo)
	buildGradleAndroidStatus := target(build, pipeline.TargetGradleAndroid)
	buildXcodeStatus := target(build, pipeline.TargetXcodeIOS)
	cargoSBOMStatus := target(publish, pipeline.TargetCargoContainerFirst)
	goSBOMStatus := target(publish, pipeline.TargetGoContainerFirst)
	sbomStatus := target(publish, pipeline.TargetSBOM)
	npmPackageName := topLevelJSONString(in.DevArtifactsJSON, "npm_package_name")
	npmPackageVersion := topLevelJSONString(in.DevArtifactsJSON, "npm_package_version")
	npmPublishStatus := topLevelJSONString(in.DevArtifactsJSON, "npm_publish_status")

	_, _ = fmt.Fprintf(w, "================================================\n")
	_, _ = fmt.Fprintf(w, "Generating Dev Release Summary\n")
	_, _ = fmt.Fprintf(w, "================================================\n")
	_, _ = fmt.Fprintf(w, "Project Type: %s\n", in.ProjectType)
	_, _ = fmt.Fprintf(w, "Branch: %s\n", in.ReleaseRef)
	_, _ = fmt.Fprintf(w, "Commit: %s\n", short)

	pkgLabel := npmPackageName
	if pkgLabel == "" {
		pkgLabel = "none"
	}

	verLabel := npmPackageVersion
	if verLabel == "" {
		verLabel = "none"
	}

	_, _ = fmt.Fprintf(w, "NPM Package: %s@%s\n\n", pkgLabel, verLabel)

	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "# Dev Release Summary\n\n")
	_, _ = fmt.Fprintf(&b, "## Build Information\n")
	_, _ = fmt.Fprintf(&b, "| Property | Value |\n")
	_, _ = fmt.Fprintf(&b, "|----------|-------|\n")
	_, _ = fmt.Fprintf(&b, "| **Project Type** | `%s` |\n", in.ProjectType)
	_, _ = fmt.Fprintf(&b, "| **Branch** | `%s` |\n", in.ReleaseRef)
	_, _ = fmt.Fprintf(&b, "| **Commit** | `%s` |\n", short)
	_, _ = fmt.Fprintf(&b, "| **Built By** | @%s |\n", in.ReleaseActor)
	_, _ = fmt.Fprintf(&b, "| **Built At** | %s |\n\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))
	_, _ = fmt.Fprintf(&b, "## Job Status\n")
	_, _ = fmt.Fprintf(&b, "| Job | Status |\n")
	_, _ = fmt.Fprintf(&b, "|-----|--------|\n")
	_, _ = fmt.Fprintf(&b, "| Build Maven | %s |\n", domainsummary.StatusIcon(buildMavenStatus))
	_, _ = fmt.Fprintf(&b, "| Build NPM | %s |\n", domainsummary.StatusIcon(buildNPMStatus))
	_, _ = fmt.Fprintf(&b, "| Build Gradle | %s |\n", domainsummary.StatusIcon(buildGradleStatus))
	_, _ = fmt.Fprintf(&b, "| Build Go | %s |\n", domainsummary.StatusIcon(buildGoStatus))
	_, _ = fmt.Fprintf(&b, "| Build Cargo | %s |\n", domainsummary.StatusIcon(buildCargoStatus))
	_, _ = fmt.Fprintf(&b, "| Build Gradle Android | %s |\n", domainsummary.StatusIcon(buildGradleAndroidStatus))
	_, _ = fmt.Fprintf(&b, "| Build Xcode | %s |\n", domainsummary.StatusIcon(buildXcodeStatus))
	_, _ = fmt.Fprintf(&b, "| Build Container | %s |\n", domainsummary.StatusIcon(containerStatus))

	if in.ProjectType == projecttype.NPM {
		row := fmt.Sprintf("| Publish NPM Package | %s |\n", domainsummary.StatusIcon(npmStatus))
		if npmPublishStatus == "already-exists" {
			row = fmt.Sprintf("| Publish NPM Package | %s (already published — skipped) |\n",
				domainsummary.StatusIcon(npmStatus))
		}

		b.WriteString(row)
	}

	_, _ = fmt.Fprintf(&b, "| Cargo SBOM | %s |\n", domainsummary.StatusIcon(cargoSBOMStatus))
	_, _ = fmt.Fprintf(&b, "| Go SBOM | %s |\n", domainsummary.StatusIcon(goSBOMStatus))
	_, _ = fmt.Fprintf(&b, "| Dev SBOMs | %s |\n", domainsummary.StatusIcon(sbomStatus))

	_, _ = fmt.Fprintf(&b, "\n## Published Artifacts\n")

	if in.ProjectType == projecttype.NPM {
		if npmPackageName != "" && npmPackageVersion != "" && npmStatus == string(domainsummary.ResultSuccess) {
			_, _ = fmt.Fprintf(&b, "\n### NPM Package\n")

			if npmPublishStatus == "already-exists" {
				_, _ = fmt.Fprintf(&b, "> **Note:** Version already existed in registry — publish was skipped (same commit SHA).\n\n")
			}

			_, _ = fmt.Fprintf(&b, "```\n%s@%s\n```\n\n", npmPackageName, npmPackageVersion)
			_, _ = fmt.Fprintf(&b, "```bash\nnpm install %s@%s\nnpm install %s@dev\n```\n",
				npmPackageName, npmPackageVersion, npmPackageName)
		} else {
			_, _ = fmt.Fprintf(&b, "\n### NPM Package\nNot published\n")
		}
	}

	_, _ = fmt.Fprintf(&b, "\n## Resources\n")
	_, _ = fmt.Fprintf(&b, "- [Packages](%s)\n",
		domainsummary.PackagesURL(in.Platform, in.ServerURL, in.ReleaseRepository))
	_, _ = fmt.Fprintf(&b, "- [Workflow Run](%s)\n\n", in.RunURL)
	_, _ = fmt.Fprintf(&b, "These are development artifacts tagged with `dev` and are not intended for production use.\n")

	if err := sink.Append(ctx, b.String()); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(w, "✓ Dev release summary generated successfully\n")

	return nil
}

func topLevelJSONString(value, key string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}

	var doc map[string]string
	if err := json.Unmarshal([]byte(value), &doc); err != nil {
		return ""
	}

	return doc[key]
}
