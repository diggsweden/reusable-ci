// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"context"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/build"
	"github.com/diggsweden/reusable-ci/internal/domain/ci"
)

// XcodeBuildInput drives `summary xcode-build`. Mirrors
// scripts/summary/write-xcode-build-summary.sh.
type XcodeBuildInput struct {
	XcodeVersion  string
	Scheme        string
	Configuration string
	Destination   string
	Signing       bool
	Version       string
	BuildNumber   string
	IPAName       string

	// Now is baked in for deterministic testing. Empty → time.Now().
	Now time.Time
}

// XcodeBuild appends the Xcode build summary block to the step
// summary. Pure rendering lives in domain/build.RenderXcodeSummary.
func XcodeBuild(ctx context.Context, sink ci.SummarySink, in XcodeBuildInput) error {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	configuration := in.Configuration
	if configuration == "" {
		configuration = "Release"
	}
	destination := in.Destination
	if destination == "" {
		destination = "generic/platform=iOS"
	}
	version := in.Version
	if version == "" {
		version = "unknown"
	}
	md := build.RenderXcodeSummary(build.XcodeSummaryInput{
		XcodeVersion:  in.XcodeVersion,
		Scheme:        in.Scheme,
		Configuration: configuration,
		Destination:   destination,
		Signing:       in.Signing,
		Version:       version,
		BuildNumber:   in.BuildNumber,
		IPAName:       in.IPAName,
	}, now)
	return sink.Append(ctx, md)
}
