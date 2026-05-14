// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"context"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/build"
	"github.com/diggsweden/reusable-ci/internal/domain/ci"
)

// NPMBuildInput drives `summary npm-build`. Mirrors
// scripts/summary/write-npm-build-summary.sh.
type NPMBuildInput struct {
	PackageName string
	Version     string
	NodeVersion string
	SkipTests   bool

	// Now is baked in for deterministic testing. Empty → time.Now().
	Now time.Time
}

// NPMBuild appends the NPM build summary block to the step summary.
func NPMBuild(ctx context.Context, sink ci.SummarySink, in NPMBuildInput) error {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	md := build.RenderNPMSummary(build.NPMSummaryInput{
		PackageName: in.PackageName,
		Version:     in.Version,
		NodeVersion: in.NodeVersion,
		SkipTests:   in.SkipTests,
	}, now)
	return sink.Append(ctx, md)
}
