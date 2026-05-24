// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"context"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/publish"
)

// GooglePlayUploadInput drives `summary google-play-upload`.
type GooglePlayUploadInput struct {
	AABFile         string
	PackageName     string
	Track           string
	Status          string
	ReleaseName     string
	UserFraction    float64
	UserFractionSet bool // true → render a Staged Rollout row
	Priority        int

	// Now is baked in for deterministic testing. Empty → time.Now().
	Now time.Time
}

// GooglePlayUpload appends the Google Play upload summary block to the
// step summary. Pure rendering lives in
// domain/publish.RenderGooglePlayUploadSummary.
func GooglePlayUpload(ctx context.Context, sink ci.SummarySink, in GooglePlayUploadInput) error {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}

	return sink.Append(ctx, publish.RenderGooglePlayUploadSummary(publish.GooglePlayUploadInput{
		AABFile:         in.AABFile,
		PackageName:     in.PackageName,
		Track:           in.Track,
		Status:          in.Status,
		ReleaseName:     in.ReleaseName,
		UserFraction:    in.UserFraction,
		UserFractionSet: in.UserFractionSet,
		Priority:        in.Priority,
	}, now))
}
