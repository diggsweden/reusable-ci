// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

// errEscapingDir is returned instead of writing when a download is aimed
// outside the working directory. The double writes real files where it is
// told to, and where it is told comes from the transfer plan's path, so
// without this the suite's own safety would rest on the product's
// safe-relative-path guard holding. A regression there should fail a test,
// not create directories next to the developer's temp dir.
var errEscapingDir = errors.New("download directory escapes the working directory")

func (d *assembleDistDownloader) DownloadRunArtifact(_ context.Context, in provider.RunArtifactDownload) (provider.RunArtifactInfo, error) {
	d.calls = append(d.calls, in)

	if filepath.IsAbs(in.Dir) || slices.Contains(strings.Split(filepath.ToSlash(filepath.Clean(in.Dir)), "/"), "..") {
		return provider.RunArtifactInfo{}, fmt.Errorf("%w: %s", errEscapingDir, in.Dir)
	}

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

// TestAssembleDist_MergesLedgersKeepingFirstSeenOrder covers the release image
// ledger merge: several inputs, named individually or as a directory to search,
// combined into the one file a release publishes.
//
// The images are c,b and b,a, which is deliberate. Merging keeps the order the
// inputs were listed in and drops later duplicates, so the result is c,b,a --
// a fixture of a,b and b,c would merge to a,b,c under that rule and also under
// sorting, proving neither. The merged file is published, so its bytes being
// determined by the input order rather than by chance is the point.
func TestAssembleDist_MergesLedgersKeepingFirstSeenOrder(t *testing.T) {
	chdirTempForAssembleDist(t)
	writeFileForAssembleDist(t, "ledgers/a.json", `[{"image":"c"},{"image":"b"}]`+"\n")
	writeFileForAssembleDist(t, "ledgers/nested/one/release-images.json", `[{"image":"b"},{"image":"a"}]`+"\n")

	var stderr bytes.Buffer

	_, err := apprelease.AssembleDist(context.Background(), &assembleDistDownloader{}, fakeoutputsink.New(t), &stderr, apprelease.AssembleDistInput{
		ArtifactNames:       "build",
		Path:                "dist/",
		LedgerFiles:         "ledgers/a.json\nmissing-ledger.json\nledgers/nested",
		LedgerExpectedCount: 3,
		ReleaseImagesPath:   "dist/release-images.json",
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("keeps input order and drops the duplicate", func(t *testing.T) {
		want := `[{"image":"c"},{"image":"b"},{"image":"a"}]` + "\n"
		if got := readFileForAssembleDist(t, "dist/release-images.json"); got != want {
			t.Errorf("merged ledger = %q, want %q", got, want)
		}
	})

	t.Run("an input that is not there is a warning, not a failure", func(t *testing.T) {
		if !strings.Contains(stderr.String(), "release image ledger input not found") {
			t.Errorf("stderr = %q", stderr.String())
		}
	})

	t.Run("the expected count is checked against the merged images", func(t *testing.T) {
		// Four entries were read and three survive deduplication. The run
		// asked for three and was accepted, so the guard counts what ships
		// rather than what was read -- otherwise this would have failed.
		if !strings.Contains(stderr.String(), "release image ledger contains 3 images") {
			t.Errorf("stderr = %q", stderr.String())
		}
	})
}

// TestAssembleDist_PrunesDirectoriesBeforeTheDigestIsTaken covers --prune-dirs:
// directories under the hand-off are removed, files beside them are kept, and
// the removal happens before the digest so pruned content cannot appear in it.
//
// The ordering claim is checked by comparing two runs rather than asserting a
// fixed hash. A tree with a nested directory must digest identically to the
// same tree without one; if the prune ran after the digest, the nested file
// would be folded in and the two would differ. The test used to name the
// ordering and never look at the digest at all.
func TestAssembleDist_PrunesDirectoriesBeforeTheDigestIsTaken(t *testing.T) {
	assembleWith := func(t *testing.T, files map[string]string) apprelease.AssembleDistResult {
		t.Helper()
		chdirTempForAssembleDist(t)

		for name, body := range files {
			writeFileForAssembleDist(t, name, body)
		}

		res, err := apprelease.AssembleDist(context.Background(), &assembleDistDownloader{}, fakeoutputsink.New(t), nil, apprelease.AssembleDistInput{
			ArtifactNames: "build",
			Path:          "dist/",
			PruneDirs:     true,
		})
		if err != nil {
			t.Fatal(err)
		}

		return res
	}

	var withNested, withoutNested apprelease.AssembleDistResult

	t.Run("directories go, files stay", func(t *testing.T) {
		withNested = assembleWith(t, map[string]string{
			"dist/nested/file": "must be pruned\n",
			"dist/top.txt":     "keep\n",
		})

		if _, err := os.Stat("dist/nested"); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("nested directory survived prune: %v", err)
		}

		if _, err := os.Stat("dist/top.txt"); err != nil {
			t.Errorf("top-level file was pruned: %v", err)
		}

		// The downloaded artifact lands in the hand-off as a top-level file.
		// Pruning must leave it, or the release would ship without it.
		if _, err := os.Stat("dist/build.txt"); err != nil {
			t.Errorf("downloaded artifact was pruned: %v", err)
		}
	})

	t.Run("the pruned content is not in the digest", func(t *testing.T) {
		withoutNested = assembleWith(t, map[string]string{"dist/top.txt": "keep\n"})

		if withNested.Digest == "" {
			t.Fatal("no digest returned")
		}

		if withNested.Digest != withoutNested.Digest {
			t.Errorf("digest with a pruned directory = %s, without = %s; pruned content reached the digest",
				withNested.Digest, withoutNested.Digest)
		}
	})
}

// TestAssembleDist_RefusesBadInput covers each way the inputs can be rejected,
// and where in the pipeline each refusal happens.
//
// AssembleDist is a linear hand-off: validate, download, merge, prune, digest.
// Input shape and path safety are settled before anything is fetched, so those
// rows require the downloader untouched. The ledger checks sit after the
// download by design -- in a real run the ledger files arrive with the
// artifacts, so they cannot be read any earlier -- and those rows say so
// rather than pretending nothing happened.
//
// Each row gets its own working directory and its own downloader. They used to
// share both, along with any fixture files an earlier row had written.
func TestAssembleDist_RefusesBadInput(t *testing.T) {
	for _, tc := range []struct {
		name         string
		files        map[string]string
		in           apprelease.AssembleDistInput
		want         error
		fetchedFirst bool // the refusal comes after the download stage
	}{
		{
			name: "no artifact names and no transfer plan",
			in:   apprelease.AssembleDistInput{Path: "dist/"},
			want: errs.ErrUsage,
		},
		{
			name: "artifact names and a transfer plan together",
			in:   apprelease.AssembleDistInput{Path: "dist/", ArtifactNames: "build", ArtifactTransferPlanJSON: `{"version":1,"items":[]}`},
			want: errs.ErrUsage,
		},
		{
			name: "release images path climbs out of the workspace",
			in:   apprelease.AssembleDistInput{Path: "dist/", ArtifactNames: "build", ReleaseImagesPath: "../release-images.json"},
			want: errs.ErrUsage,
		},
		{
			name: "transfer item path climbs out of the workspace",
			in:   apprelease.AssembleDistInput{Path: "dist/", ArtifactTransferPlanJSON: `{"version":1,"items":[{"kind":"build_artifact","name":"build","path":"../dist","required":true}]}`},
			want: errs.ErrUsage,
		},
		{
			name:         "ledger is not an array",
			files:        map[string]string{"bad.json": `{"image":"a"}`},
			in:           apprelease.AssembleDistInput{Path: "dist/", ArtifactNames: "build", LedgerFiles: "bad.json"},
			want:         errs.ErrInvalidConfig,
			fetchedFirst: true,
		},
		{
			name:         "ledger holds fewer images than expected",
			files:        map[string]string{"one.json": `[{"image":"a"}]`},
			in:           apprelease.AssembleDistInput{Path: "dist/", ArtifactNames: "build", LedgerFiles: "one.json", LedgerExpectedCount: 2},
			want:         errs.ErrValidation,
			fetchedFirst: true,
		},
		{
			name:         "a ledger count is expected but no ledger given",
			in:           apprelease.AssembleDistInput{Path: "dist/", ArtifactNames: "build", LedgerExpectedCount: 1},
			want:         errs.ErrUsage,
			fetchedFirst: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chdirTempForAssembleDist(t)

			for name, body := range tc.files {
				writeFileForAssembleDist(t, name, body)
			}

			dl := &assembleDistDownloader{}

			_, err := apprelease.AssembleDist(context.Background(), dl, fakeoutputsink.New(t), nil, tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}

			if fetched := len(dl.calls) != 0; fetched != tc.fetchedFirst {
				t.Errorf("fetched before refusing = %v, want %v (calls: %+v)", fetched, tc.fetchedFirst, dl.calls)
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
