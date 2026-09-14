// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assertPrivateEvidenceOutput(t *testing.T, staged, published string) {
	t.Helper()
	require.NotEqual(t, published, staged)
	require.Equal(t, filepath.Base(published), filepath.Base(staged))
	require.Equal(t, filepath.Dir(published), filepath.Dir(filepath.Dir(staged)))
	_, err := os.Stat(staged)
	require.ErrorIs(t, err, os.ErrNotExist)
	body, err := os.ReadFile(published)
	require.NoError(t, err)
	require.NotEmpty(t, body)
}

func TestEvidenceBoundary_RefusesCollisionsBeforeTools(t *testing.T) {
	t.Parallel()

	for _, multi := range []bool{false, true} {
		root := t.TempDir()
		buildah, skopeo := &fakeImageEvidenceBuildah{}, &fakeImageEvidenceSkopeo{}
		trivy, syft := &fakeImageEvidenceTrivy{}, &fakeImageEvidenceSyft{}

		in := appcontainer.ImageEvidenceInput{LocalImageRef: "localhost/app:v1", TrivyOutput: filepath.Join(root, "report.json"), SBOMOutput: root + "/./report.json", TempDir: root}
		if multi {
			in = appcontainer.ImageEvidenceInput{LocalManifest: "localhost/app:v1", Platforms: []string{"linux/amd64", "linux/arm64"}, TrivyOutputTemplate: root + "/report-{arch}.json", SBOMOutputTemplate: root + "/./report-{arch}.json", TempDir: root}
		}

		err := appcontainer.ImageEvidence(t.Context(), buildah, skopeo, trivy, syft, io.Discard, io.Discard, in)
		require.ErrorIs(t, err, errs.ErrUsage)
		require.Empty(t, buildah.imageRef)
		require.Empty(t, buildah.manifest)
		require.Empty(t, skopeo.registryCopies)
		require.Empty(t, trivy.calls)
		require.Empty(t, syft.targets)
	}
}

func TestEvidenceBoundary_RequiresFreshCompleteReports(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"missing-trivy", "missing-sbom", "malformed-sbom", "success"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			layout := filepath.Join(root, "layout")
			require.NoError(t, os.Mkdir(layout, 0o700))

			trivyPath, sbomPath := filepath.Join(root, "trivy.json"), filepath.Join(root, "sbom.json")
			require.NoError(t, os.WriteFile(trivyPath, []byte(`{"Results":[],"old":true}`), 0o600))
			require.NoError(t, os.WriteFile(sbomPath, []byte(`{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,"old":true}`), 0o600))
			before := snapshotContainerTree(t, root)
			trivy := &fakeImageEvidenceTrivy{noOutput: kind == "missing-trivy"}

			syft := &fakeImageEvidenceSyft{noOutput: kind == "missing-sbom"}
			if kind == "malformed-sbom" {
				syft.jsonBody = `{"bomFormat":"SPDX","specVersion":"1.6","version":1}`
			}

			err := appcontainer.ImageEvidence(t.Context(), nil, nil, trivy, syft, io.Discard, io.Discard, appcontainer.ImageEvidenceInput{OCILayout: layout, TrivyOutput: trivyPath, SBOMOutput: sbomPath})
			if kind != "success" {
				require.ErrorIs(t, err, errs.ErrMalformedInput)
				require.Equal(t, before, snapshotContainerTree(t, root))
			} else {
				require.NoError(t, err)
				body, err := os.ReadFile(sbomPath)
				require.NoError(t, err)
				require.JSONEq(t, `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1}`, string(body))
				assertPrivateEvidenceOutput(t, trivyArg(t, trivy.args, "--output"), trivyPath)
			}
		})
	}
}

func TestEvidenceBoundary_ValidatesSeparateRegistryInputs(t *testing.T) {
	t.Parallel()

	for _, pair := range [][2]string{{"https://registry.example/app", "sha256:" + strings.Repeat("a", 64)}, {"registry.example/app", "sha256:bad"}, {"registry.example/a/../app", "sha256:" + strings.Repeat("a", 64)}} {
		root := t.TempDir()
		skopeo := &fakeImageEvidenceSkopeo{}
		err := appcontainer.ImageEvidence(t.Context(), nil, skopeo, &fakeImageEvidenceTrivy{}, &fakeImageEvidenceSyft{}, io.Discard, io.Discard, appcontainer.ImageEvidenceInput{RegistryRef: pair[0], Digest: pair[1], Platforms: []string{"linux/amd64"}, TrivyOutputTemplate: root + "/report-{arch}.json", TempDir: root})
		require.ErrorIs(t, err, errs.ErrUsage)
		require.Empty(t, skopeo.registryCopies)
	}
}
