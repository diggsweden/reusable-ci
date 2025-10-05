// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom_test

import (
	"bytes"
	appsbom "github.com/diggsweden/reusable-ci/v3/internal/app/sbom"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSBOMPlanBoundary_EmptyCannotReportStaleOutput(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	stale := filepath.Join(dir, "app-1.0.0-build-sbom.cyclonedx.json")
	require.NoError(t, os.WriteFile(stale, []byte("old"), 0o600))

	var log bytes.Buffer

	syft := &fakeSyft{}
	err := appsbom.Generate(t.Context(), syft, nil, &fakeGit{}, nil, &log, io.Discard, appsbom.GenerateInput{Layers: " , \t ", Name: "app", Version: "1.0.0"})
	require.ErrorIs(t, err, errs.ErrUsage)
	require.Empty(t, syft.calls)
	require.Empty(t, log.String())

	body, err := os.ReadFile(stale)
	require.NoError(t, err)
	require.Equal(t, "old", string(body))
}

func TestSBOMIdentityBoundary_RejectsNondigestsBeforeGeneration(t *testing.T) {
	t.Chdir(t.TempDir())

	for _, digest := range []string{"latest", strings.Repeat("a", 64), "sha256:abc", "sha256:" + strings.Repeat("A", 64)} {
		syft := &fakeSyft{}
		err := appsbom.GenerateContainer(t.Context(), syft, nil, &fakeGit{}, io.Discard, io.Discard, appsbom.GenerateContainerInput{RefName: "v1", Repo: "org/app", ImageName: "registry.example/app", ImageDigest: digest})
		require.ErrorIs(t, err, errs.ErrUsage)
		require.Empty(t, syft.calls)
	}
}
