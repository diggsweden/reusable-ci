// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package build holds pure helpers shared by the toolchain build wrappers
// (maven, gradle, npm, gradle-android, xcode-ios).
package build

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/vifraa/gopom"
)

// IsSnapshot reports whether a Maven version string is a -SNAPSHOT.
// Mirrors check `[[ $VERSION == *-SNAPSHOT ]]`.
func IsSnapshot(version string) bool {
	return strings.HasSuffix(version, "-SNAPSHOT")
}

// POM is the subset of pom.xml fields the build/sbom use cases consume.
//
// Maven inheritance: when a child POM omits version/groupId, they're
// inherited from <parent>. The parser surfaces both so callers can apply
// the inheritance fall-through.
type POM struct {
	GroupID    string
	ArtifactID string
	Version    string
	Parent     POMParent
}

// POMParent is the inheritance source for omitted top-level fields.
type POMParent struct {
	GroupID    string
	ArtifactID string
	Version    string
}

// ParsePOM extracts the typed fields the build/sbom use cases need from a
// pom.xml. Parsing is delegated to github.com/vifraa/gopom (Renovate-tracked)
// so all of the XML/namespace edge cases are handled by a focused library.
//
// On top of the raw parse we apply Maven's inheritance rule: when
// <groupId>/<version> is omitted at the top level, the value from <parent>
// is used. ArtifactID is never inherited.
//
// Property interpolation (<version>${revision}</version>) is NOT resolved
// here — the caller decides whether to fall back to `mvn help:evaluate`
// when POMHasUnresolvedProperty(field) reports true. ~95% of POMs use
// literal values; the fallback path stays available for the rest.
func ParsePOM(body []byte) (POM, error) {
	project, err := gopom.ParseFromReader(bytes.NewReader(body))
	if err != nil {
		return POM{}, fmt.Errorf("parse pom.xml: %w", err)
	}

	pom := POM{
		GroupID:    derefString(project.GroupID),
		ArtifactID: derefString(project.ArtifactID),
		Version:    derefString(project.Version),
	}
	if project.Parent != nil {
		pom.Parent = POMParent{
			GroupID:    derefString(project.Parent.GroupID),
			ArtifactID: derefString(project.Parent.ArtifactID),
			Version:    derefString(project.Parent.Version),
		}
	}
	// Apply inheritance for omitted child fields. ArtifactID is never
	// inherited — every POM has its own.
	if pom.GroupID == "" {
		pom.GroupID = pom.Parent.GroupID
	}

	if pom.Version == "" {
		pom.Version = pom.Parent.Version
	}

	return pom, nil
}

// derefString returns the trimmed string a points at, or "" if a is nil.
// gopom uses *string for every optional XML element; this normalises into
// the value shape the rest of the domain consumes.
func derefString(a *string) string {
	if a == nil {
		return ""
	}

	return strings.TrimSpace(*a)
}

// POMHasUnresolvedProperty reports whether value carries a Maven property
// reference (${name}) that ParsePOM intentionally does not expand.
// Callers use this to decide whether to fall back to `mvn help:evaluate`.
func POMHasUnresolvedProperty(value string) bool {
	return strings.Contains(value, "${")
}

// MavenSummaryInput drives RenderMavenSummary.
type MavenSummaryInput struct {
	BuildType   string
	GroupID     string
	ArtifactID  string
	Version     string
	JavaVersion string
	SkipTests   bool
	IsSnapshot  bool
}

// RenderMavenSummary returns the markdown block written by.
func RenderMavenSummary(in MavenSummaryInput, now time.Time) string {
	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "## Maven Build Summary 🔨\n")
	_, _ = fmt.Fprintf(&b, "\n")
	_, _ = fmt.Fprintf(&b, "- **Type:** %s\n", in.BuildType)
	_, _ = fmt.Fprintf(&b, "- **Artifact:** `%s:%s:%s`\n", in.GroupID, in.ArtifactID, in.Version)
	_, _ = fmt.Fprintf(&b, "- **Java:** %s\n", in.JavaVersion)

	if in.SkipTests {
		_, _ = fmt.Fprintf(&b, "- **Tests:** ⊘ Skipped\n")
	} else {
		_, _ = fmt.Fprintf(&b, "- **Tests:** ✓ Executed\n")
	}

	_, _ = fmt.Fprintf(&b, "- **Snapshot:** %t\n", in.IsSnapshot)
	_, _ = fmt.Fprintf(&b, "\n")
	_, _ = fmt.Fprintf(&b, "*Build completed at %s*\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))

	return b.String()
}
