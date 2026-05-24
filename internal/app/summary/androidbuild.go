// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"context"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/build"
	"github.com/diggsweden/reusable-ci/internal/domain/ci"
)

// AndroidBuildInput drives `summary android-build`.
type AndroidBuildInput struct {
	JavaVersion string
	JDKDist     string
	BuildModule string
	Flavor      string
	BuildTypes  string
	IncludeAAB  bool
	Signing     bool
	SkipTests   bool
	Version     string
	VersionCode string
	DebugName   string
	ReleaseName string
	AABName     string

	// Now is baked in for deterministic testing. Empty → time.Now().
	Now time.Time
}

// AndroidBuild appends the Android variants build summary block to the
// step summary. Pure rendering lives in
// domain/build.RenderAndroidSummary.
func AndroidBuild(ctx context.Context, sink ci.SummarySink, in AndroidBuildInput) error {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}

	md := build.RenderAndroidSummary(build.AndroidSummaryInput{
		JavaVersion: in.JavaVersion,
		JDKDist:     in.JDKDist,
		BuildModule: in.BuildModule,
		Flavor:      in.Flavor,
		BuildTypes:  in.BuildTypes,
		IncludeAAB:  in.IncludeAAB,
		Signing:     in.Signing,
		SkipTests:   in.SkipTests,
		Version:     in.Version,
		VersionCode: in.VersionCode,
		DebugName:   in.DebugName,
		ReleaseName: in.ReleaseName,
		AABName:     in.AABName,
	}, now)

	return sink.Append(ctx, md)
}
