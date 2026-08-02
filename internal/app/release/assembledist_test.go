// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type assembleDistDownloader struct {
	calls []provider.RunArtifactDownload
	fail  map[string]error
}

func (d *assembleDistDownloader) DownloadRunArtifact(_ context.Context, in provider.RunArtifactDownload) (provider.RunArtifactInfo, error) {
	d.calls = append(d.calls, in)
	if err := d.fail[in.Name]; err != nil {
		return provider.RunArtifactInfo{}, err
	}

	if err := os.MkdirAll(in.Dir, 0o755); err != nil { //nolint:gosec,mnd // test fixture.
		return provider.RunArtifactInfo{}, err
	}

	if err := os.WriteFile(filepath.Join(in.Dir, in.Name+".txt"), []byte(in.Name+"\n"), 0o644); err != nil { //nolint:gosec,mnd // test fixture.
		return provider.RunArtifactInfo{}, err
	}

	return provider.RunArtifactInfo{Name: in.Name, FileCount: 1}, nil
}

func TestAssembleDist_DownloadsNamedArtifactsAndWritesDigest(t *testing.T) {
	chdirTempForAssembleDist(t)

	dl := &assembleDistDownloader{}
	sink := fakeoutputsink.New(t)

	got, err := apprelease.AssembleDist(context.Background(), dl, sink, nil, apprelease.AssembleDistInput{
		ArtifactNames: "build-a\nbuild-b",
		Path:          "dist/",
	})
	if err != nil {
		t.Fatal(err)
	}

	want, err := apprelease.DistDigest("dist")
	if err != nil {
		t.Fatal(err)
	}

	if got.Digest != want || sink.Single("digest") != want {
		t.Fatalf("digest result = %q sink = %q want %q", got.Digest, sink.Single("digest"), want)
	}

	if len(dl.calls) != 2 || dl.calls[0].Name != "build-a" || dl.calls[0].Dir != "dist/" || dl.calls[1].Name != "build-b" {
		t.Fatalf("download calls = %+v", dl.calls)
	}
}

func TestAssembleDist_DownloadsTransferPlanAndWarnsForOptionalFailures(t *testing.T) {
	chdirTempForAssembleDist(t)

	dl := &assembleDistDownloader{fail: map[string]error{"missing-123": errors.New("not found")}} //nolint:err113 // test mock error.

	var stderr bytes.Buffer

	plan := `{"version":1,"items":[{"kind":"build_artifact","name":"build","path":"dist/","required":true},{"kind":"analyzed_container_sbom","name_template":"missing-{run_id}","path":"sboms/","required":false}]}`

	_, err := apprelease.AssembleDist(context.Background(), dl, fakeoutputsink.New(t), &stderr, apprelease.AssembleDistInput{
		ArtifactTransferPlanJSON: plan,
		Path:                     "dist/",
		RunID:                    "123",
		Repository:               "owner/repo",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(dl.calls) != 2 || dl.calls[0].Repository != "owner/repo" || dl.calls[1].Name != "missing-123" {
		t.Fatalf("download calls = %+v", dl.calls)
	}

	if !strings.Contains(stderr.String(), "optional artifact 'missing-123' was not downloaded: not found") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestAssembleDist_MergesReleaseImageLedgers(t *testing.T) {
	chdirTempForAssembleDist(t)
	writeFileForAssembleDist(t, "ledgers/a.json", "[{\"image\":\"a\"},{\"image\":\"b\"}]\n")
	writeFileForAssembleDist(t, "ledgers/nested/one/release-images.json", "[{\"image\":\"b\"},{\"image\":\"c\"}]\n")

	dl := &assembleDistDownloader{}

	var stderr bytes.Buffer

	_, err := apprelease.AssembleDist(context.Background(), dl, fakeoutputsink.New(t), &stderr, apprelease.AssembleDistInput{
		ArtifactNames:            "build",
		Path:                     "dist/",
		LedgerFiles:              "ledgers/a.json\nmissing-ledger.json\nledgers/nested",
		LedgerExpectedCount:      3,
		ReleaseImagesPath:        "dist/release-images.json",
		ArtifactTransferPlanJSON: "",
	})
	if err != nil {
		t.Fatal(err)
	}

	got := readFileForAssembleDist(t, "dist/release-images.json")
	if got != `[{"image":"a"},{"image":"b"},{"image":"c"}]`+"\n" {
		t.Fatalf("merged ledger = %q", got)
	}

	if !strings.Contains(stderr.String(), "release image ledger input not found") || !strings.Contains(stderr.String(), "release image ledger contains 3 images") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestAssembleDist_PrunesTopLevelDirectoriesBeforeDigest(t *testing.T) {
	chdirTempForAssembleDist(t)

	dl := &assembleDistDownloader{}

	if _, err := apprelease.AssembleDist(context.Background(), dl, fakeoutputsink.New(t), nil, apprelease.AssembleDistInput{
		ArtifactNames: "build",
		Path:          "dist/",
		PruneDirs:     true,
	}); err != nil {
		t.Fatal(err)
	}

	writeFileForAssembleDist(t, "dist/nested/file", "must be pruned\n")
	writeFileForAssembleDist(t, "dist/top.txt", "keep\n")

	if _, err := apprelease.AssembleDist(context.Background(), dl, fakeoutputsink.New(t), nil, apprelease.AssembleDistInput{
		ArtifactNames: "build",
		Path:          "dist/",
		PruneDirs:     true,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat("dist/nested/file"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("nested directory survived prune: %v", err)
	}

	if _, err := os.Stat("dist/top.txt"); err != nil {
		t.Fatalf("top-level file was pruned: %v", err)
	}
}

func TestAssembleDist_Validation(t *testing.T) {
	chdirTempForAssembleDist(t)

	dl := &assembleDistDownloader{}

	for _, tc := range []struct {
		name string
		in   apprelease.AssembleDistInput
		want error
	}{
		{name: "missing artifact names", in: apprelease.AssembleDistInput{Path: "dist/"}, want: errs.ErrUsage},
		{name: "mutually exclusive inputs", in: apprelease.AssembleDistInput{Path: "dist/", ArtifactNames: "build", ArtifactTransferPlanJSON: `{"version":1,"items":[]}`}, want: errs.ErrUsage},
		{name: "unsafe release images path", in: apprelease.AssembleDistInput{Path: "dist/", ArtifactNames: "build", ReleaseImagesPath: "../release-images.json"}, want: errs.ErrUsage},
		{name: "unsafe transfer path", in: apprelease.AssembleDistInput{Path: "dist/", ArtifactTransferPlanJSON: `{"version":1,"items":[{"kind":"build_artifact","name":"build","path":"../dist","required":true}]}`}, want: errs.ErrUsage},
		{name: "non-array ledger", in: apprelease.AssembleDistInput{Path: "dist/", ArtifactNames: "build", LedgerFiles: "bad.json"}, want: errs.ErrInvalidConfig},
		{name: "ledger count mismatch", in: apprelease.AssembleDistInput{Path: "dist/", ArtifactNames: "build", LedgerFiles: "one.json", LedgerExpectedCount: 2}, want: errs.ErrValidation},
		{name: "ledger expected without files", in: apprelease.AssembleDistInput{Path: "dist/", ArtifactNames: "build", LedgerExpectedCount: 1}, want: errs.ErrUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "non-array ledger" {
				writeFileForAssembleDist(t, "bad.json", `{"image":"a"}`)
			}

			if tc.name == "ledger count mismatch" {
				writeFileForAssembleDist(t, "one.json", `[{"image":"a"}]`)
			}

			_, err := apprelease.AssembleDist(context.Background(), dl, fakeoutputsink.New(t), nil, tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func chdirTempForAssembleDist(t *testing.T) {
	t.Helper()

	t.Chdir(t.TempDir())
}

func writeFileForAssembleDist(t *testing.T, path, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec,mnd // test fixture.
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil { //nolint:gosec,mnd // test fixture.
		t.Fatal(err)
	}
}

func readFileForAssembleDist(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path) //nolint:gosec // test fixture.
	if err != nil {
		t.Fatal(err)
	}

	return string(body)
}
