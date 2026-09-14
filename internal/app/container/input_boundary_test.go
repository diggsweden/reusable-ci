// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/stretchr/testify/require"
)

func TestContainerfileBoundary_StopsBeforeLateOnlyARG(t *testing.T) {
	t.Parallel()

	for _, from := range []string{"FROM alpine", "from alpine", "FrOm alpine", "FROM\talpine", " \tFROM alpine"} {
		t.Run(from, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "Containerfile")
			require.NoError(t, os.WriteFile(path, []byte(from+"\nARG BASE=late\n"), 0o600))
			got, err := appcontainer.ContainerfileArgDefault(appcontainer.ContainerfileArgDefaultInput{File: path, Name: "BASE"})
			require.ErrorIs(t, err, errs.ErrValidation)
			require.Empty(t, got)
			require.NoError(t, os.WriteFile(path, []byte("ARG BASE=early\n"+from+"\nARG BASE=late\n"), 0o600))
			got, err = appcontainer.ContainerfileArgDefault(appcontainer.ContainerfileArgDefaultInput{File: path, Name: "BASE"})
			require.NoError(t, err)
			require.Equal(t, "early", got)
		})
	}
}

func TestBinaryBoundary_RefusesNamedDirectoriesAndAliases(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"directory", "file-link", "directory-link"} {
		root := t.TempDir()
		dir := filepath.Join(root, "binaries")
		require.NoError(t, os.Mkdir(dir, 0o700))

		source := filepath.Join(dir, "app")
		if kind == "directory" {
			require.NoError(t, os.Mkdir(source, 0o700))
		} else {
			target := filepath.Join(root, "target")
			if kind == "file-link" {
				require.NoError(t, os.WriteFile(target, []byte("canary"), 0o600))
			} else {
				require.NoError(t, os.Mkdir(target, 0o700))
			}

			require.NoError(t, os.Symlink(target, source))
		}

		before := snapshotContainerTree(t, root)
		err := appcontainer.SuffixExtractedBinaries(io.Discard, appcontainer.SuffixExtractedBinariesInput{Dir: dir, Arch: "amd64", ExpectedNames: "app"})
		require.ErrorIs(t, err, errs.ErrValidation)
		require.Equal(t, before, snapshotContainerTree(t, root))
	}
}

func TestArtifactDirectoryBoundary_MissingIsNotEmpty(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, kind := range []string{"missing", "empty", "file"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(root, kind)
			switch kind {
			case "empty":
				require.NoError(t, os.Mkdir(path, 0o700))
			case "file":
				require.NoError(t, os.WriteFile(path, []byte("canary"), 0o600))
			}

			var log bytes.Buffer

			err := appcontainer.ValidateArtifacts(&log, output.Annotator{}, appcontainer.ValidateArtifactsInput{ProjectType: "go", ArtifactDir: path})
			require.Error(t, err)

			switch kind {
			case "missing":
				require.ErrorIs(t, err, errs.ErrMissingInput)
				require.EqualValues(t, 66, errs.ExitCodeFromError(err))
				require.Empty(t, log.String())
			case "empty":
				require.ErrorIs(t, err, errs.ErrValidation)
				require.Contains(t, log.String(), "Expected binaries")
			default:
				require.NotErrorIs(t, err, errs.ErrMissingInput)
				require.Empty(t, log.String())
			}
		})
	}
}

func TestDigestMarkerBoundary_RejectsNormalizedCollisionsAndTypes(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"case", "directory", "link"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "digests")
			require.NoError(t, os.Mkdir(dir, 0o700))

			lower := strings.Repeat("a", 64)
			require.NoError(t, os.WriteFile(filepath.Join(dir, lower), nil, 0o600))
			other := filepath.Join(dir, strings.Repeat("b", 64))

			switch kind {
			case "case":
				require.NoError(t, os.WriteFile(filepath.Join(dir, strings.ToUpper(lower)), nil, 0o600))
			case "directory":
				require.NoError(t, os.Mkdir(other, 0o700))
			case "link":
				require.NoError(t, os.Symlink(lower, other))
			}

			registry := &fakeManifestRegistry{}
			err := appcontainer.MergeManifest(t.Context(), registry, io.Discard, appcontainer.MergeManifestInput{ImageName: "registry.example/app", Tags: "registry.example/app:v1", DigestsDir: dir})
			require.ErrorIs(t, err, errs.ErrValidation)
			require.Zero(t, registry.mergeCalls)
		})
	}
}

func TestReleaseLabelBoundary_RequiresValuesAndRFC3339(t *testing.T) {
	t.Parallel()

	valid := appcontainer.OCIReleaseLabelsInput{OCILabels: domaincontainer.OCILabels{Title: "app", Version: "v1", Revision: strings.Repeat("a", 40), RefName: "v1", Documentation: "https://example.test/docs"}, Source: "https://example.test/repo", Created: "2026-08-01T02:03:04Z"}

	for _, field := range []string{"title", "version", "revision", "ref", "source", "documentation", "created"} {
		for _, value := range []string{"", " \t "} {
			in := valid

			switch field {
			case "title":
				in.Title = value
			case "version":
				in.Version = value
			case "revision":
				in.Revision = value
			case "ref":
				in.RefName = value
			case "source":
				in.Source = value
			case "documentation":
				in.Documentation = value
			case "created":
				in.Created = value
			}

			if field == "documentation" && value == "" {
				continue
			} // Empty documentation defaults to Source#readme.

			labels, err := appcontainer.OCIReleaseLabels(in)
			require.ErrorIs(t, err, errs.ErrUsage, field)
			require.Empty(t, labels)
		}
	}

	for _, created := range []string{"yesterday", "2026-08-01", "2026-08-01T02:03:04", "2026-13-01T02:03:04Z"} {
		in := valid
		in.Created = created
		_, err := appcontainer.OCIReleaseLabels(in)
		require.ErrorIs(t, err, errs.ErrUsage)
	}

	labels, err := appcontainer.OCIReleaseLabels(valid)
	require.NoError(t, err)
	require.Contains(t, labels, "org.opencontainers.image.created=2026-08-01T02:03:04Z")
}
