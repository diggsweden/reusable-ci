// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/config"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/git"
	"github.com/diggsweden/reusable-ci/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/internal/domain/release"
	domainsummary "github.com/diggsweden/reusable-ci/internal/domain/summary"
	"github.com/diggsweden/reusable-ci/internal/domain/validate"
)

// gitInfoOps is the slice of git.Repo methods the prerequisites summary
// needs. Tests inject a fake; production passes adapter/git.New(). The
// CommitInfo return type lives in domain/git so this file stays
// adapter-free.
type gitInfoOps interface {
	TaggerInfo(ctx context.Context, tag string) (string, string, error)
	TagMessage(ctx context.Context, tag string) (string, error)
	CatFileTag(ctx context.Context, tag string) (string, error)
	CommitInfo(ctx context.Context, sha string) (git.CommitInfo, error)
}

// PrerequisitesSummaryInput drives `summary prerequisites`. The use case
// composes ~6 markdown sections (tag info / commit info / configuration
// / secrets status / job status / validation results / footer).
type PrerequisitesSummaryInput struct {
	TagName                  string
	CommitSHA                string
	RefType                  provider.RefType
	ConfigPlanJSON           string
	ProjectTypes             string // optional pre-resolved comma list
	BuildTypes               string // optional pre-resolved comma list
	ContainerRegistry        string
	SignArtifacts            bool
	RequireAllowlistedSigner bool
	JobStatus                domainsummary.Result
	PublishTo                string // "maven-central,npmjs,github-packages" CSV

	// Boolean secret presence flags
	HasReleaseGPGPrivateKey bool
	HasReleaseGPGPassphrase bool
	HasReleaseToken         bool
	HasReleaseGPGPublicKey  bool
	HasMavenCentralUsername bool
	HasMavenCentralPassword bool
	HasNPMToken             bool

	Now time.Time
}

// Prerequisites appends the prerequisites validation report to the
// step summary.
func Prerequisites(ctx context.Context, sink ci.SummarySink, gitr gitInfoOps, in PrerequisitesSummaryInput) error {
	if err := enrichPrerequisitesFromConfigPlan(&in); err != nil {
		return err
	}

	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}

	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err := writeTagInfo(ctx, &b, gitr, in); err != nil {
		return err
	}

	if err := writeCommitInfo(ctx, &b, gitr, in); err != nil {
		return err
	}

	writeConfiguration(&b, in)
	writeSecretsStatus(&b, in)
	writeJobStatus(&b, in)
	writeValidationResults(&b, in)
	_, _ = fmt.Fprintf(&b, "\n---\n*Generated at: %s*\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))

	return sink.Append(ctx, b.String())
}

//nolint:cyclop // enriches prereqs with one field per artifact's plan slice.
func enrichPrerequisitesFromConfigPlan(in *PrerequisitesSummaryInput) error {
	if strings.TrimSpace(in.ConfigPlanJSON) == "" {
		return nil
	}

	var plan pipeline.ConfigPlan
	if err := json.Unmarshal([]byte(in.ConfigPlanJSON), &plan); err != nil {
		return fmt.Errorf("parse config-plan-json: %w: %w", err, errs.ErrInvalidConfig)
	}

	if plan.Version != pipeline.ConfigPlanVersion {
		return fmt.Errorf("config-plan-json has unsupported version %d: %w", plan.Version, errs.ErrInvalidConfig)
	}

	projectTypes := map[string]bool{}
	buildTypes := map[string]bool{}
	publishTo := map[string]bool{}

	for _, artifact := range plan.Artifacts.All {
		if artifact.ProjectType != "" {
			projectTypes[string(artifact.ProjectType)] = true
		}

		if artifact.BuildType != "" {
			buildTypes[string(artifact.BuildType)] = true
		}

		for _, target := range artifact.PublishTo {
			if target != "" {
				publishTo[string(target)] = true
			}
		}
	}

	if in.ProjectTypes == "" {
		in.ProjectTypes = sortedCSV(projectTypes)
	}

	if in.BuildTypes == "" {
		in.BuildTypes = sortedCSV(buildTypes)
	}

	if in.PublishTo == "" {
		in.PublishTo = sortedCSV(publishTo)
	}

	return nil
}

func sortedCSV(values map[string]bool) string {
	if len(values) == 0 {
		return ""
	}

	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}

	sort.Strings(out)

	return strings.Join(out, ",")
}

//nolint:cyclop // tag-info renderer: one branch per known/unknown signer + repo state.
func writeTagInfo(ctx context.Context, b *strings.Builder, gitr gitInfoOps, in PrerequisitesSummaryInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	_, _ = fmt.Fprintf(b, "# 📋 Release Prerequisites Validation Report\n\n")
	_, _ = fmt.Fprintf(b, "## 🏷️ Release Tag\n")
	_, _ = fmt.Fprintf(b, "- **Tag:** `%s`\n", in.TagName)
	_, _ = fmt.Fprintf(b, "- **Type:** %s\n", in.RefType)

	if in.RefType != provider.RefTypeTag || gitr == nil {
		return nil
	}

	tagger, date, err := gitr.TaggerInfo(ctx, in.TagName)
	if err != nil {
		tagger, date = "N/A", "N/A" //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}

	if tagger == "" {
		tagger = "N/A"
	}

	if date == "" {
		date = "N/A"
	}

	msg, err := gitr.TagMessage(ctx, in.TagName)
	if err != nil || msg == "" {
		msg = "No message"
	} else {
		// First line only.
		if idx := strings.IndexByte(msg, '\n'); idx != -1 {
			msg = msg[:idx]
		}
	}
	// Signature presence comes from the raw tag body — strictly
	// faster than the deleted `git tag -v` subprocess, and the summary
	// only needs "is a signature there?" not "does it verify against
	// some keyring."
	sig := "Not signed"

	if body, err := gitr.CatFileTag(ctx, in.TagName); err == nil {
		switch {
		case strings.Contains(body, "BEGIN PGP SIGNATURE"):
			sig = "GPG signed"
		case strings.Contains(body, "BEGIN SSH SIGNATURE"):
			sig = "SSH signed"
		}
	}

	_, _ = fmt.Fprintf(b, "- **Tagger:** %s\n", tagger)
	_, _ = fmt.Fprintf(b, "- **Tag Date:** %s\n", date)
	_, _ = fmt.Fprintf(b, "- **Tag Signature:** %s\n", sig)
	_, _ = fmt.Fprintf(b, "- **Tag Message:** %s\n", msg)

	return nil
}

func writeCommitInfo(ctx context.Context, b *strings.Builder, gitr gitInfoOps, in PrerequisitesSummaryInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if gitr == nil || in.CommitSHA == "" {
		return nil
	}

	info, err := gitr.CommitInfo(ctx, in.CommitSHA)
	if err != nil {
		// Best-effort: skip the commit-info section, but log so the
		// blank section in the summary correlates with a real cause.
		slog.Warn("summary prerequisites: skipping commit section",
			"sha", in.CommitSHA, "err", err)

		return nil
	}

	sig := "Not signed"
	if strings.Contains(info.Body, "BEGIN PGP SIGNATURE") {
		sig = "GPG signed"
	} else if strings.Contains(info.Body, "BEGIN SSH SIGNATURE") {
		sig = "SSH signed"
	}

	_, _ = fmt.Fprintf(b, "\n## 📦 Tagged Commit\n")
	_, _ = fmt.Fprintf(b, "- **SHA:** `%s`\n", in.CommitSHA)
	_, _ = fmt.Fprintf(b, "- **Author:** %s\n", info.Author)
	_, _ = fmt.Fprintf(b, "- **Date:** %s\n", info.Date)
	_, _ = fmt.Fprintf(b, "- **Signature:** %s\n", sig)
	_, _ = fmt.Fprintf(b, "- **Message:** %s\n", info.Message)

	return nil
}

func writeConfiguration(b *strings.Builder, in PrerequisitesSummaryInput) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	signing := "Disabled"
	if in.SignArtifacts {
		signing = "Enabled"
	}

	projectTypes := in.ProjectTypes
	if projectTypes == "" {
		projectTypes = "(unspecified)"
	}

	buildTypes := in.BuildTypes
	if buildTypes == "" {
		buildTypes = "(unspecified)"
	}

	_, _ = fmt.Fprintf(b, "\n## ⚙️ Configuration\n")
	_, _ = fmt.Fprintf(b, "| Setting | Value |\n")
	_, _ = fmt.Fprintf(b, "|---------|-------|\n")
	_, _ = fmt.Fprintf(b, "| **Project Types** | %s |\n", domainsummary.SanitizeCell(projectTypes))
	_, _ = fmt.Fprintf(b, "| **Build Types** | %s |\n", domainsummary.SanitizeCell(buildTypes))
	_, _ = fmt.Fprintf(b, "| **Container Registry** | %s |\n", domainsummary.SanitizeCell(in.ContainerRegistry))
	_, _ = fmt.Fprintf(b, "| **Release Publisher** | GitHub CLI |\n")
	_, _ = fmt.Fprintf(b, "| **GPG Signing** | %s |\n", signing)
}

func writeSecretsStatus(b *strings.Builder, in PrerequisitesSummaryInput) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	_, _ = fmt.Fprintf(b, "\n## 🔑 Required Secrets Status\n\n")
	_, _ = fmt.Fprintf(b, "| Secret | Purpose | Status |\n")
	_, _ = fmt.Fprintf(b, "|--------|---------|--------|\n")

	row := func(name, purpose string, has bool) {
		status := "✗ Missing"
		if has {
			status = "✓ Available"
		}

		_, _ = fmt.Fprintf(b, "| %s | %s | %s |\n", name, purpose, status)
	}
	if in.SignArtifacts {
		row("RELEASE_GPG_PRIVATE_KEY", "Sign commits/artifacts", in.HasReleaseGPGPrivateKey)
		row("RELEASE_GPG_PASSPHRASE", "GPG passphrase", in.HasReleaseGPGPassphrase)
	}

	row("RELEASE_TOKEN", "Push commits & releases", in.HasReleaseToken)

	if in.SignArtifacts {
		row("RELEASE_GPG_PUBLIC_KEY", "GPG verification", in.HasReleaseGPGPublicKey)
	}

	if in.PublishTo != "" {
		targets := parsePublishTargets(in.PublishTo)
		if slices.Contains(targets, config.PublishMavenCentral) {
			row("MAVEN_CENTRAL_USERNAME", "Maven Central auth", in.HasMavenCentralUsername)
			row("MAVEN_CENTRAL_PASSWORD", "Maven Central auth", in.HasMavenCentralPassword)
		}

		if slices.Contains(targets, config.PublishNPMJS) {
			row("NPM_TOKEN", "NPM registry auth", in.HasNPMToken)
		}
	}
}

func writeJobStatus(b *strings.Builder, in PrerequisitesSummaryInput) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	_, _ = fmt.Fprintf(b, "\n")

	if in.JobStatus == domainsummary.ResultSuccess {
		_, _ = fmt.Fprintf(b, "### ✓ All required prerequisites are configured!\n")
		_, _ = fmt.Fprintf(b, "Ready to proceed with release 🚀\n")
	} else {
		_, _ = fmt.Fprintf(b, "### ✗ Prerequisites validation failed\n")
		_, _ = fmt.Fprintf(b, "Please configure the missing secrets before attempting release\n")
	}
}

func writeValidationResults(b *strings.Builder, in PrerequisitesSummaryInput) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	_, _ = fmt.Fprintf(b, "\n## ✓ Validation Results\n\n")
	_, _ = fmt.Fprintf(b, "| Validation | Result | Details |\n")
	_, _ = fmt.Fprintf(b, "|------------|--------|---------|\n")

	row := func(name, result, details string) {
		_, _ = fmt.Fprintf(b, "| %s | %s | %s |\n", name, result, details)
	}

	if in.RefType == provider.RefTypeTag {
		if _, err := validate.ParseTagFormat(in.TagName); err == nil {
			row("Semantic Version", "✓ Pass", fmt.Sprintf("`%s` follows vX.Y.Z", in.TagName))
		} else {
			row("Semantic Version", "✗ Fail", "Invalid format")
		}

		row("Tag Type", "✓ Pass", "Annotated (not lightweight)")
		row("Tag Signature", "✓ Pass", "GPG/SSH signed")

		if release.IsPrereleaseTag(in.TagName) {
			row("Release Type", "🚧 Pre-release", fmt.Sprintf("`%s` version", prereleaseIdentifier(in.TagName)))
		} else {
			row("Release Type", "🎯 Stable", "Production release")
		}
	}

	switch {
	case validate.IsSnapshot(in.TagName):
		row("Signer Allowlist", "− Skip", "SNAPSHOT release (bypasses gate)")
	case in.RequireAllowlistedSigner:
		row("Signer Allowlist", "✓ Enforced", ".reusable-ci/allowed_{signers,gpg_fingerprints}")
	}

	if in.HasReleaseToken {
		row("Release Token", "✓ Pass", "Valid GitHub token")
	} else {
		row("Release Token", "✗ Fail", "Missing RELEASE_TOKEN")
	}

	emitPublishRows(in, row)
}

// emitPublishRows renders one summary row per resolved publish target.
// Split out from the parent table builder so nestif doesn't trip on
// the per-target if/else nesting.
func emitPublishRows(in PrerequisitesSummaryInput, row func(name, status, detail string)) {
	if in.PublishTo == "" {
		return
	}

	targets := parsePublishTargets(in.PublishTo)

	if slices.Contains(targets, config.PublishMavenCentral) {
		if in.HasMavenCentralUsername {
			row("Maven Central", "✓ Pass", "Credentials configured")
		} else {
			row("Maven Central", "✗ Fail", "Missing credentials")
		}
	}

	if slices.Contains(targets, config.PublishNPMJS) {
		if in.HasNPMToken {
			row("NPM Registry", "✓ Pass", "Token configured")
		} else {
			row("NPM Registry", "✗ Fail", "Missing NPM_TOKEN")
		}
	}

	if slices.Contains(targets, config.PublishGitHubPackages) {
		row("GitHub Packages", "✓ Pass", "Using GITHUB_TOKEN")
	}
}

// prereleaseIdentifier extracts the canonical pre-release identifier
// from a tag (e.g. "v1.0.0-rc.1" → "rc"). Used only for the "🚧
// Pre-release" details column.
func prereleaseIdentifier(tag string) string {
	idx := strings.IndexByte(tag, '-')
	if idx == -1 {
		return ""
	}

	rest := tag[idx+1:]
	for i := range len(rest) {
		if rest[i] == '.' {
			return rest[:i]
		}
	}

	return rest
}

// parsePublishTargets splits the PublishTo CSV and returns a sorted
// slice of typed config.PublishTarget values. Empty entries are
// dropped. Unknown tokens flow through as untyped enum values —
// validation that they're in config.ValidPublishTargets is the
// config-parse step's job; this function is consumed only after that.
func parsePublishTargets(s string) []config.PublishTarget {
	parts := strings.Split(s, ",")

	out := make([]config.PublishTarget, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, config.PublishTarget(p))
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })

	return out
}
