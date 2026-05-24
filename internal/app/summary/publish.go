// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
)

// MavenCentralPublishInput drives MavenCentralPublish.
type MavenCentralPublishInput struct {
	Version    string
	IsSnapshot bool
	Now        time.Time
}

// MavenCentralPublish appends the Maven Central publish summary block.
func MavenCentralPublish(ctx context.Context, sink ci.SummarySink, in MavenCentralPublishInput) error {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}

	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "## Published to Maven Central 🚀\n\n")
	_, _ = fmt.Fprintf(&b, "- **Version:** %s\n", in.Version)

	if in.IsSnapshot {
		_, _ = fmt.Fprintf(&b, "- **Type:** SNAPSHOT\n")
	} else {
		_, _ = fmt.Fprintf(&b, "- **Type:** Release\n")
	}

	_, _ = fmt.Fprintf(&b, "- **Registry:** [Maven Central](https://central.sonatype.com/)\n\n")

	if in.IsSnapshot {
		_, _ = fmt.Fprintf(&b, "⚠️ **SNAPSHOT** releases are available immediately in the snapshot repository.\n")
	} else {
		_, _ = fmt.Fprintf(&b, "✓ **Release** deployed to staging. Will be published to Central within 30 minutes.\n")
	}

	_, _ = fmt.Fprintf(&b, "\n*Published at %s*\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))

	return sink.Append(ctx, b.String())
}

// GitHubPackagesPublishInput drives GitHubPackagesPublish.
type GitHubPackagesPublishInput struct {
	Repository  string
	PackageType string
	Now         time.Time
}

// GitHubPackagesPublish appends the GitHub Packages publish summary block.
func GitHubPackagesPublish(ctx context.Context, sink ci.SummarySink, in GitHubPackagesPublishInput) error {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}

	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "## Published to GitHub Packages 📦\n\n")
	_, _ = fmt.Fprintf(&b, "- **Package Type:** %s\n", in.PackageType)
	_, _ = fmt.Fprintf(&b, "- **Registry:** GitHub Packages\n")
	_, _ = fmt.Fprintf(&b, "- **Repository:** [%s](https://github.com/%s/packages)\n\n", in.Repository, in.Repository)
	_, _ = fmt.Fprintf(&b, "*Published at %s*\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))

	return sink.Append(ctx, b.String())
}
