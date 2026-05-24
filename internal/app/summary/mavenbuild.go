// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"context"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/build"
	"github.com/diggsweden/reusable-ci/internal/domain/ci"
)

// MavenBuildInput drives `summary maven-build`.
type MavenBuildInput struct {
	BuildType   string
	GroupID     string
	ArtifactID  string
	Version     string
	JavaVersion string
	SkipTests   bool
	IsSnapshot  bool

	// Now is baked in for deterministic testing. Empty → time.Now().
	Now time.Time
}

// MavenBuild appends the Maven build summary block to the step summary.
// Pure rendering lives in domain/build.RenderMavenSummary.
func MavenBuild(ctx context.Context, sink ci.SummarySink, in MavenBuildInput) error {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}

	md := build.RenderMavenSummary(build.MavenSummaryInput{
		BuildType:   in.BuildType,
		GroupID:     in.GroupID,
		ArtifactID:  in.ArtifactID,
		Version:     in.Version,
		JavaVersion: in.JavaVersion,
		SkipTests:   in.SkipTests,
		IsSnapshot:  in.IsSnapshot,
	}, now)

	return sink.Append(ctx, md)
}
