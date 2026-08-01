// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
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

// ForgePackagesPublishInput drives ForgePackagesPublish.
type ForgePackagesPublishInput struct {
	Repository  string
	PackageType string
	// RegistryName is the forge-native registry's display name (e.g.
	// "GitHub Packages", "GitLab Package Registry"). Empty falls back to a
	// forge-neutral label — the publish target is the forge's own registry,
	// resolved per forge, so this block must not hard-code GitHub.
	RegistryName string
	Now          time.Time
}

// ForgePackagesPublish appends the forge-native package-registry publish
// summary block (GitHub Packages / GitLab Package Registry / Forgejo packages).
func ForgePackagesPublish(ctx context.Context, sink ci.SummarySink, in ForgePackagesPublishInput) error {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}

	registry := in.RegistryName
	if registry == "" {
		registry = "the forge package registry"
	}

	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "## Published to %s 📦\n\n", registry)
	_, _ = fmt.Fprintf(&b, "- **Package Type:** %s\n", in.PackageType)
	_, _ = fmt.Fprintf(&b, "- **Registry:** %s\n", registry)
	_, _ = fmt.Fprintf(&b, "- **Repository:** %s\n\n", in.Repository)
	_, _ = fmt.Fprintf(&b, "*Published at %s*\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))

	return sink.Append(ctx, b.String())
}
