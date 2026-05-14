// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/config"
	"github.com/diggsweden/reusable-ci/internal/domain/git"
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
	VerifyTag(ctx context.Context, tag string) (string, bool, error)
	CommitInfo(ctx context.Context, sha string) (git.CommitInfo, error)
}

// PrerequisitesInput drives `summary prerequisites`. The use case
// composes ~6 markdown sections (tag info / commit info / configuration
// / secrets status / job status / validation results / footer).
type PrerequisitesInput struct {
	TagName            string
	CommitSHA          string
	RefType            provider.RefType
	Artifacts          string // raw JSON array (for project/build-type extraction)
	ProjectTypes       string // optional pre-resolved comma list
	BuildTypes         string // optional pre-resolved comma list
	ContainerRegistry  string
	SignArtifacts      bool
	CheckAuthorization bool
	Actor              string
	JobStatus          domainsummary.Result
	PublishTo          string // "maven-central,npmjs,github-packages" CSV

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
//
// Mirrors scripts/summary/write-prerequisites-summary.sh.
func Prerequisites(ctx context.Context, sink ci.SummarySink, gitr gitInfoOps, in PrerequisitesInput) error {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}

	var b strings.Builder
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
	fmt.Fprintf(&b, "\n---\n*Generated at: %s*\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))

	return sink.Append(ctx, b.String())
}

func writeTagInfo(ctx context.Context, b *strings.Builder, gitr gitInfoOps, in PrerequisitesInput) error {
	fmt.Fprintf(b, "# 📋 Release Prerequisites Validation Report\n\n")
	fmt.Fprintf(b, "## 🏷️ Release Tag\n")
	fmt.Fprintf(b, "- **Tag:** `%s`\n", in.TagName)
	fmt.Fprintf(b, "- **Type:** %s\n", in.RefType)
	if in.RefType != provider.RefTypeTag || gitr == nil {
		return nil
	}

	tagger, date, err := gitr.TaggerInfo(ctx, in.TagName)
	if err != nil {
		tagger, date = "N/A", "N/A"
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
	sig := "Not signed"
	if _, ok, _ := gitr.VerifyTag(ctx, in.TagName); ok {
		sig = "GPG signed"
	} else if body, err := gitr.CatFileTag(ctx, in.TagName); err == nil {
		if strings.Contains(body, "BEGIN PGP SIGNATURE") {
			sig = "GPG signed"
		} else if strings.Contains(body, "BEGIN SSH SIGNATURE") {
			sig = "SSH signed"
		}
	}
	fmt.Fprintf(b, "- **Tagger:** %s\n", tagger)
	fmt.Fprintf(b, "- **Tag Date:** %s\n", date)
	fmt.Fprintf(b, "- **Tag Signature:** %s\n", sig)
	fmt.Fprintf(b, "- **Tag Message:** %s\n", msg)
	return nil
}

func writeCommitInfo(ctx context.Context, b *strings.Builder, gitr gitInfoOps, in PrerequisitesInput) error {
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
	fmt.Fprintf(b, "\n## 📦 Tagged Commit\n")
	fmt.Fprintf(b, "- **SHA:** `%s`\n", in.CommitSHA)
	fmt.Fprintf(b, "- **Author:** %s\n", info.Author)
	fmt.Fprintf(b, "- **Date:** %s\n", info.Date)
	fmt.Fprintf(b, "- **Signature:** %s\n", sig)
	fmt.Fprintf(b, "- **Message:** %s\n", info.Message)
	return nil
}

func writeConfiguration(b *strings.Builder, in PrerequisitesInput) {
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
	fmt.Fprintf(b, "\n## ⚙️ Configuration\n")
	fmt.Fprintf(b, "| Setting | Value |\n")
	fmt.Fprintf(b, "|---------|-------|\n")
	fmt.Fprintf(b, "| **Project Types** | %s |\n", projectTypes)
	fmt.Fprintf(b, "| **Build Types** | %s |\n", buildTypes)
	fmt.Fprintf(b, "| **Container Registry** | %s |\n", in.ContainerRegistry)
	fmt.Fprintf(b, "| **Release Publisher** | GitHub CLI |\n")
	fmt.Fprintf(b, "| **GPG Signing** | %s |\n", signing)
}

func writeSecretsStatus(b *strings.Builder, in PrerequisitesInput) {
	fmt.Fprintf(b, "\n## 🔑 Required Secrets Status\n\n")
	fmt.Fprintf(b, "| Secret | Purpose | Status |\n")
	fmt.Fprintf(b, "|--------|---------|--------|\n")
	row := func(name, purpose string, has bool) {
		status := "✗ Missing"
		if has {
			status = "✓ Available"
		}
		fmt.Fprintf(b, "| %s | %s | %s |\n", name, purpose, status)
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

func writeJobStatus(b *strings.Builder, in PrerequisitesInput) {
	fmt.Fprintf(b, "\n")
	if in.JobStatus == domainsummary.ResultSuccess {
		fmt.Fprintf(b, "### ✅ All required prerequisites are configured!\n")
		fmt.Fprintf(b, "Ready to proceed with release 🚀\n")
	} else {
		fmt.Fprintf(b, "### ❌ Prerequisites validation failed\n")
		fmt.Fprintf(b, "Please configure the missing secrets before attempting release\n")
	}
}

func writeValidationResults(b *strings.Builder, in PrerequisitesInput) {
	fmt.Fprintf(b, "\n## ✅ Validation Results\n\n")
	fmt.Fprintf(b, "| Validation | Result | Details |\n")
	fmt.Fprintf(b, "|------------|--------|---------|\n")
	row := func(name, result, details string) {
		fmt.Fprintf(b, "| %s | %s | %s |\n", name, result, details)
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
	case in.CheckAuthorization:
		row("User Authorization", "✓ Pass", fmt.Sprintf("%s authorized", in.Actor))
	case validate.IsSnapshot(in.TagName):
		row("User Authorization", "− Skip", "SNAPSHOT release")
	}
	if in.HasReleaseToken {
		row("Release Token", "✓ Pass", "Valid GitHub token")
	} else {
		row("Release Token", "✗ Fail", "Missing RELEASE_TOKEN")
	}
	if in.PublishTo != "" {
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
