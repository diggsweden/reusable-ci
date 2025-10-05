// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package artifact_test

import (
	"context"
	"errors"
	"testing"

	appartifact "github.com/diggsweden/reusable-ci/v3/internal/app/artifact"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

// fakeArtifacts implements both run-artifact roles and records the input.
type fakeArtifacts struct {
	gotUpload   provider.RunArtifactUpload
	gotDownload provider.RunArtifactDownload
	downloads   int
	info        provider.RunArtifactInfo
	err         error
}

func (f *fakeArtifacts) UploadRunArtifact(_ context.Context, in provider.RunArtifactUpload) (provider.RunArtifactInfo, error) {
	f.gotUpload = in

	return f.info, f.err
}

func (f *fakeArtifacts) DownloadRunArtifact(_ context.Context, in provider.RunArtifactDownload) (provider.RunArtifactInfo, error) {
	f.downloads++
	f.gotDownload = in

	return f.info, f.err
}

func TestDownload_HappyPath(t *testing.T) {
	t.Parallel()

	fake := &fakeArtifacts{info: provider.RunArtifactInfo{Name: "dist", ID: "42", Bytes: 123, FileCount: 3}}
	sink := fakeoutputsink.New(t)

	got, err := appartifact.Download(context.Background(), fake, sink, nil, provider.RunArtifactDownload{Name: "dist", Dir: "out"})
	if err != nil {
		t.Fatal(err)
	}

	if got.ID != "42" || fake.gotDownload.Name != "dist" || fake.gotDownload.Dir != "out" {
		t.Errorf("download did not pass input through: %+v", fake.gotDownload)
	}

	if sink.Single("artifact-name") != "dist" || sink.Single("artifact-id") != "42" {
		t.Errorf("sink missing artifact keys: name=%q id=%q", sink.Single("artifact-name"), sink.Single("artifact-id"))
	}
}

func TestDownload_PropagatesOutputSinkFailure(t *testing.T) {
	t.Parallel()

	sink := fakeoutputsink.New(t)
	if err := sink.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	fake := &fakeArtifacts{info: provider.RunArtifactInfo{Name: "dist", ID: "42"}}

	_, err := appartifact.Download(context.Background(), fake, sink, nil, provider.RunArtifactDownload{Name: "dist", Dir: "out"})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("output failure = %v, want ErrValidation", err)
	}
}

func TestDownload_Validation(t *testing.T) {
	t.Parallel()

	sink := fakeoutputsink.New(t)
	ctx := context.Background()

	// No selector at all is a usage error (exit 2), symmetric with Upload —
	// not a data-level validation failure.
	if _, err := appartifact.Download(ctx, &fakeArtifacts{}, sink, nil, provider.RunArtifactDownload{Dir: "out"}); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("no selector: err = %v, want ErrUsage", err)
	}

	// Both selectors is also a usage error.
	if _, err := appartifact.Download(ctx, &fakeArtifacts{}, sink, nil, provider.RunArtifactDownload{Name: "dist", Pattern: "d*", Dir: "out"}); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("both selectors: err = %v, want ErrUsage", err)
	}

	// A *given* but malformed name (multi-line) stays a data-level validation
	// failure — distinct from a missing one.
	if _, err := appartifact.Download(ctx, &fakeArtifacts{}, sink, nil, provider.RunArtifactDownload{Name: "bad\nname", Dir: "out"}); !errors.Is(err, errs.ErrValidation) {
		t.Errorf("malformed name: err = %v, want ErrValidation", err)
	}

	if _, err := appartifact.Download(ctx, &fakeArtifacts{}, sink, nil, provider.RunArtifactDownload{Name: "dist", Dir: ""}); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("empty dir: err = %v, want ErrUsage", err)
	}
}

func TestUpload_Validation(t *testing.T) {
	t.Parallel()

	sink := fakeoutputsink.New(t)
	ctx := context.Background()

	// Neither dir nor files.
	if _, err := appartifact.Upload(ctx, &fakeArtifacts{}, sink, nil, provider.RunArtifactUpload{Name: "dist"}); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("no dir/files: err = %v, want ErrUsage", err)
	}

	if _, err := appartifact.Upload(ctx, &fakeArtifacts{}, sink, nil, provider.RunArtifactUpload{Name: "a\nb", Dir: "d"}); !errors.Is(err, errs.ErrValidation) {
		t.Errorf("bad name: err = %v, want ErrValidation", err)
	}
}

// TestUpload_DefaultsIfNoFilesPolicy proves the safe default is applied
// when the caller leaves the policy empty.
func TestUpload_DefaultsIfNoFilesPolicy(t *testing.T) {
	t.Parallel()

	fake := &fakeArtifacts{info: provider.RunArtifactInfo{Name: "dist"}}

	if _, err := appartifact.Upload(context.Background(), fake, fakeoutputsink.New(t), nil, provider.RunArtifactUpload{Name: "dist", Dir: "d"}); err != nil {
		t.Fatal(err)
	}

	if fake.gotUpload.IfNoFiles != provider.IfNoFilesError {
		t.Errorf("IfNoFiles default = %q, want %q", fake.gotUpload.IfNoFiles, provider.IfNoFilesError)
	}
}

// TestUpload_RetentionPolicy: zero (the forge default) and a positive value
// reach the uploader unchanged; a negative retention is a usage error and no
// upload is attempted.
func TestUpload_RetentionPolicy(t *testing.T) {
	t.Parallel()

	for days, wantErr := range map[int]bool{0: false, 30: false, -1: true} {
		fake := &fakeArtifacts{info: provider.RunArtifactInfo{Name: "dist"}}
		_, err := appartifact.Upload(context.Background(), fake, fakeoutputsink.New(t), nil, provider.RunArtifactUpload{Name: "dist", Dir: "d", RetentionDays: days})

		if wantErr {
			if !errors.Is(err, errs.ErrUsage) || fake.gotUpload.Name != "" {
				t.Errorf("retention %d: err = %v, upload = %+v; want ErrUsage and no upload", days, err, fake.gotUpload)
			}

			continue
		}

		if err != nil || fake.gotUpload.RetentionDays != days {
			t.Errorf("retention %d: err = %v, forwarded %d", days, err, fake.gotUpload.RetentionDays)
		}
	}
}
