// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

type fakeArtifactDownloader struct {
	calls []provider.RunArtifactDownload
	fail  map[string]error
}

func (f *fakeArtifactDownloader) DownloadRunArtifact(_ context.Context, in provider.RunArtifactDownload) (provider.RunArtifactInfo, error) {
	f.calls = append(f.calls, in)
	if err := f.fail[in.Name]; err != nil {
		return provider.RunArtifactInfo{}, err
	}

	return provider.RunArtifactInfo{Name: in.Name}, nil
}

func TestDownloadArtifacts_DownloadsExactNames(t *testing.T) {
	dl := &fakeArtifactDownloader{}

	plan := `{"version":1,"items":[{"kind":"build_artifact","name":"app-build-artifacts","path":"./release-artifacts/","required":true},{"kind":"analyzed_container_sbom","name_template":"analyzed-container-sbom-{run_id}-api-amd64","path":"./sbom-artifacts/","required":false}]}`
	if err := apprelease.DownloadArtifacts(context.Background(), dl, nil, apprelease.DownloadArtifactsInput{
		ArtifactTransferPlanJSON: plan,
		RunID:                    "123",
		Repository:               "owner/repo", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}); err != nil {
		t.Fatal(err)
	}

	if len(dl.calls) != 2 {
		t.Fatalf("calls = %+v", dl.calls)
	}

	if got := dl.calls[1].Name; got != "analyzed-container-sbom-123-api-amd64" {
		t.Errorf("templated name = %q", got)
	}

	if got := dl.calls[0].Repository; got != "owner/repo" {
		t.Errorf("repository = %q", got)
	}
}

func TestDownloadArtifacts_OptionalFailuresWarn(t *testing.T) {
	dl := &fakeArtifactDownloader{fail: map[string]error{"missing": errors.New("not found")}} //nolint:err113,goconst // test mock error / generic identifier.

	var stderr bytes.Buffer

	plan := `{"version":1,"items":[{"kind":"build_sbom","name":"missing","path":"./release-artifacts/","required":false}]}`
	if err := apprelease.DownloadArtifacts(context.Background(), dl, &stderr, apprelease.DownloadArtifactsInput{ArtifactTransferPlanJSON: plan}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(stderr.String(), `optional artifact "missing"`) {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestDownloadArtifacts_RequiredFailureErrors(t *testing.T) {
	dl := &fakeArtifactDownloader{fail: map[string]error{"missing": errors.New("not found")}} //nolint:err113 // test mock error
	plan := `{"version":1,"items":[{"kind":"build_artifact","name":"missing","path":"./release-artifacts/","required":true}]}`

	err := apprelease.DownloadArtifacts(context.Background(), dl, nil, apprelease.DownloadArtifactsInput{ArtifactTransferPlanJSON: plan})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v", err)
	}
}

func TestDownloadArtifacts_RejectsMissingPlan(t *testing.T) {
	dl := &fakeArtifactDownloader{}

	err := apprelease.DownloadArtifacts(context.Background(), dl, nil, apprelease.DownloadArtifactsInput{})
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("err = %v", err)
	}

	if len(dl.calls) != 0 {
		t.Errorf("fetched without a plan: %+v", dl.calls)
	}
}

func TestDownloadArtifacts_RejectsUnsupportedPlanVersion(t *testing.T) {
	dl := &fakeArtifactDownloader{}

	// Items are present so the refusal is the version, not an empty plan.
	plan := `{"version":2,"items":[{"kind":"build_artifact","name":"app","path":"./release-artifacts/"}]}`

	err := apprelease.DownloadArtifacts(context.Background(), dl, nil, apprelease.DownloadArtifactsInput{ArtifactTransferPlanJSON: plan})
	if err == nil || !strings.Contains(err.Error(), "unsupported version 2") {
		t.Errorf("err = %v", err)
	}

	if len(dl.calls) != 0 {
		t.Errorf("fetched from a plan of an unsupported version: %+v", dl.calls)
	}
}

// TestDownloadArtifacts_RefusesAnInvalidItemBeforeFetchingIt covers the plan
// items a transfer can be told to make and must not.
//
// Every row also asserts that nothing was fetched. That is the point of
// validating an item: a plan that is refused should leave the workspace as it
// found it, not half-populated with whatever came before the bad entry.
func TestDownloadArtifacts_RefusesAnInvalidItemBeforeFetchingIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		plan string
		want string
	}{
		{
			name: "unknown kind",
			plan: `{"version":1,"items":[{"kind":"unknown","name":"app","path":"./release-artifacts/"}]}`,
			want: `artifact transfer kind "unknown" is invalid`,
		},
		{
			name: "both name and template",
			plan: `{"version":1,"items":[{"kind":"build_artifact","name":"app","name_template":"app-{run_id}","path":"./release-artifacts/"}]}`,
			want: "exactly one of name/name_template",
		},
		{
			name: "empty path",
			plan: `{"version":1,"items":[{"kind":"build_artifact","name":"app","path":""}]}`,
			want: "empty path",
		},
		{
			name: "unresolved template",
			plan: `{"version":1,"items":[{"kind":"build_artifact","name_template":"app-{other}","path":"./release-artifacts/"}]}`,
			want: "unresolved placeholders",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dl := &fakeArtifactDownloader{}

			err := apprelease.DownloadArtifacts(context.Background(), dl, nil, apprelease.DownloadArtifactsInput{
				ArtifactTransferPlanJSON: tc.plan,
				RunID:                    "123",
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v", err)
			}

			if len(dl.calls) != 0 {
				t.Errorf("fetched despite an invalid plan: %+v", dl.calls)
			}
		})
	}
}

func TestDownloadArtifacts_TemplateRequiresRunID(t *testing.T) {
	plan := `{"version":1,"items":[{"kind":"analyzed_container_sbom","name_template":"analyzed-container-sbom-{run_id}-api-amd64","path":"./sbom-artifacts/"}]}`

	err := apprelease.DownloadArtifacts(context.Background(), &fakeArtifactDownloader{}, nil, apprelease.DownloadArtifactsInput{ArtifactTransferPlanJSON: plan})
	if err == nil || !strings.Contains(err.Error(), "run-id is required") {
		t.Fatalf("err = %v", err)
	}
}
