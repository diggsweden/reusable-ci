// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/stretchr/testify/require"
)

type lateTransferRecorder struct{ calls int }

func (r *lateTransferRecorder) DownloadRunArtifact(context.Context, provider.RunArtifactDownload) (provider.RunArtifactInfo, error) {
	r.calls++

	return provider.RunArtifactInfo{}, nil
}

func TestTransferPreflight_ResolvesEveryNameBeforeDownload(t *testing.T) {
	t.Parallel()

	rec := &lateTransferRecorder{}

	var log bytes.Buffer

	err := apprelease.DownloadArtifacts(t.Context(), rec, &log, apprelease.DownloadArtifactsInput{ArtifactTransferPlanJSON: `{"version":1,"items":[{"kind":"build_artifact","name":"first","path":"dist/first","required":true},{"kind":"build_sbom","name_template":"second-{run_id}","path":"dist/second","required":true}]}`})
	require.ErrorIs(t, err, errs.ErrUsage)
	require.Zero(t, rec.calls)
	require.Empty(t, log.String())
}

func TestManifestPreflight_PreservesExistingOutput(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.Mkdir("dist", 0o700))
	require.NoError(t, os.WriteFile("dist/app.tgz", []byte("asset"), 0o600))
	require.NoError(t, os.WriteFile("dist/checksums.txt", []byte("fixture checksum"), 0o600))
	require.NoError(t, os.WriteFile("dist/artifacts.json", []byte(`[]`), 0o600))
	require.NoError(t, os.WriteFile("assets.json", []byte(`{"version":1,"assets":[{"path":"dist/app.tgz","name":"app.tgz","source":"fixture"}]}`), 0o600))
	require.NoError(t, os.WriteFile("manifest.json", []byte("keep"), 0o600))

	_, err := apprelease.WriteReleaseFileManifest(apprelease.WriteReleaseFileManifestInput{AssetsJSONFile: "assets.json", OutputFile: "manifest.json"})
	require.ErrorIs(t, err, errs.ErrValidation)
	require.Contains(t, err.Error(), "must also be present in assets")
	body, err := os.ReadFile("manifest.json")
	require.NoError(t, err)
	require.Equal(t, "keep", string(body))
}

func TestAssemblyPreflight_LateInvalidInputPreservesStaging(t *testing.T) {
	t.Chdir(t.TempDir())

	for _, dir := range []string{"release-artifacts", "release-files/assets", "release-files/sboms"} {
		require.NoError(t, os.MkdirAll(dir, 0o700))
	}

	for _, path := range []string{"release-artifacts/app.jar", "release-files/assets/keep", "release-files/sboms/keep"} {
		require.NoError(t, os.WriteFile(path, []byte("keep"), 0o600))
	}

	var log bytes.Buffer

	_, err := apprelease.Assemble(&log, apprelease.AssembleInput{ConfigPlanJSON: mustReleaseJSON(t, assemblyConfigPlan(t)), ArtifactTransferPlanJSON: `{"version":1,"items":[]}`, AttachArtifacts: "[", ProjectName: "app", Version: "v1.2.3"})
	require.ErrorIs(t, err, path.ErrBadPattern)
	require.Empty(t, log.String())

	for _, path := range []string{"release-files/assets/keep", "release-files/sboms/keep"} {
		body, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		require.Equal(t, "keep", string(body))
	}

	entries, err := os.ReadDir("release-files/assets")
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

type attachmentBindingRecorder struct{ tags, files []string }

func (r *attachmentBindingRecorder) UploadReleaseAsset(_ context.Context, tag, file string) error {
	r.tags = append(r.tags, tag)
	r.files = append(r.files, filepath.Base(file))

	return nil
}

func TestAttachmentBinding_NormalizesListsNamesAndTag(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, name := range []string{"app..tgz", "other file.tgz"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte("fixture"), 0o600))
	}

	r := &attachmentBindingRecorder{}
	err := apprelease.UploadAttachments(t.Context(), r, io.Discard, output.Annotator{}, apprelease.UploadAttachmentsInput{WorkingDir: root, Tag: "  v1.2.3  ", Pattern: "app..tgz,\nother file.tgz"})
	require.NoError(t, err)
	require.Equal(t, []string{"app..tgz", "other file.tgz"}, r.files)
	require.Equal(t, []string{"v1.2.3", "v1.2.3"}, r.tags)
}
