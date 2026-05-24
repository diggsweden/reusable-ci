// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"context"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/build"
	"github.com/diggsweden/reusable-ci/internal/domain/ci"
)

// GradleBuildInput drives `summary gradle-build`.
type GradleBuildInput struct {
	JavaVersion string
	GradleTasks string
	SkipTests   bool
	Version     string // optional; bash omits the line when empty

	// Now is baked in for deterministic testing. Empty → time.Now().
	Now time.Time
}

// GradleBuild appends the Gradle build summary block to the step
// summary. Pure rendering lives in domain/build.RenderGradleSummary.
func GradleBuild(ctx context.Context, sink ci.SummarySink, in GradleBuildInput) error {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}

	md := build.RenderGradleSummary(build.GradleSummaryInput{
		JavaVersion: in.JavaVersion,
		GradleTasks: in.GradleTasks,
		SkipTests:   in.SkipTests,
		Version:     in.Version,
	}, now)

	return sink.Append(ctx, md)
}
