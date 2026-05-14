// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"context"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/publish"
)

// AppStoreUploadInput drives `summary appstore-upload`. Mirrors
// scripts/summary/write-appstore-summary.sh.
type AppStoreUploadInput struct {
	IPAFile        string
	Platform       string
	SkipValidation bool
	SubmitReview   bool
	RequestID      string

	// Now is baked in for deterministic testing. Empty → time.Now().
	Now time.Time
}

// AppStoreUpload appends the App Store Connect upload summary block to
// the step summary. Pure rendering lives in
// domain/publish.RenderAppStoreUploadSummary.
func AppStoreUpload(ctx context.Context, sink ci.SummarySink, in AppStoreUploadInput) error {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	return sink.Append(ctx, publish.RenderAppStoreUploadSummary(publish.AppStoreUploadInput{
		IPAFile:        in.IPAFile,
		Platform:       in.Platform,
		SkipValidation: in.SkipValidation,
		SubmitReview:   in.SubmitReview,
		RequestID:      in.RequestID,
	}, now))
}
