// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package build holds pure helpers shared by the toolchain build wrappers
// (maven, gradle, npm, gradle-android, xcode-ios).
package build

import (
	"fmt"
	"strings"
	"time"
)

// IsSnapshot reports whether a Maven version string is a -SNAPSHOT.
// Mirrors the bash check `[[ $VERSION == *-SNAPSHOT ]]`.
func IsSnapshot(version string) bool {
	return strings.HasSuffix(version, "-SNAPSHOT")
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

// RenderMavenSummary returns the markdown block written by
// scripts/summary/write-maven-build-summary.sh, byte-for-byte.
func RenderMavenSummary(in MavenSummaryInput, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Maven Build Summary 🔨\n")
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "- **Type:** %s\n", in.BuildType)
	fmt.Fprintf(&b, "- **Artifact:** `%s:%s:%s`\n", in.GroupID, in.ArtifactID, in.Version)
	fmt.Fprintf(&b, "- **Java:** %s\n", in.JavaVersion)
	if in.SkipTests {
		fmt.Fprintf(&b, "- **Tests:** ⊘ Skipped\n")
	} else {
		fmt.Fprintf(&b, "- **Tests:** ✓ Executed\n")
	}
	fmt.Fprintf(&b, "- **Snapshot:** %t\n", in.IsSnapshot)
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "*Build completed at %s*\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))
	return b.String()
}
