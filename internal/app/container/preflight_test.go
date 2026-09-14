// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

// Snapshot only the owned tree, recording links without following their targets.
func snapshotContainerTree(t *testing.T, root string) map[string]string {
	t.Helper()

	handle, err := os.OpenRoot(root)
	require.NoError(t, err)

	defer func() { _ = handle.Close() }()

	files := map[string]string{}
	err = fs.WalkDir(handle.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		value := info.Mode().String()
		if info.Mode().IsRegular() {
			body, err := handle.ReadFile(path)
			if err != nil {
				return err
			}

			value += string(body)
		}

		if info.Mode()&os.ModeSymlink != 0 {
			target, err := handle.Readlink(path)
			if err != nil {
				return err
			}

			value += target
		}

		files[path] = value

		return nil
	})
	require.NoError(t, err)

	return files
}

func TestValidateArtifacts_RefusesLinkedMatches(t *testing.T) {
	t.Parallel()

	for _, project := range []string{"maven", "npm", "go", "cargo"} {
		for _, kind := range []string{"file", "directory", "root"} {
			t.Run(project+"/"+kind, func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				dir := filepath.Join(root, "artifacts")
				outside := filepath.Join(root, "outside")

				require.NoError(t, os.Mkdir(dir, 0o700))
				require.NoError(t, os.Mkdir(outside, 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(outside, "app.jar"), []byte("canary"), 0o600))

				target := filepath.Join(outside, "app.jar")
				if kind == "directory" {
					target = outside
				}

				if kind == "root" {
					dir = filepath.Join(root, "alias")
					require.NoError(t, os.Symlink(outside, dir))
				} else {
					require.NoError(t, os.Symlink(target, filepath.Join(dir, "app.jar")))
				}

				before := snapshotContainerTree(t, root)

				var log bytes.Buffer

				err := appcontainer.ValidateArtifacts(&log, output.Annotator{}, appcontainer.ValidateArtifactsInput{ProjectType: project, ArtifactDir: dir})
				require.ErrorIs(t, err, errs.ErrValidation)
				require.Empty(t, log.String())
				require.Equal(t, before, snapshotContainerTree(t, root))
			})
		}
	}
}

func TestSuffixExtractedBinaries_PreflightPreservesEntireTree(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		names, collision string
		want             error
	}{
		{"app,missing,also-missing", "", errs.ErrMissingInput},
		{"app,../outside", "", errs.ErrValidation}, {"app,/outside", "", errs.ErrValidation},
		{"app,.", "", errs.ErrValidation}, {"app,nested/name", "", errs.ErrValidation},
		{"app,nested\\name", "", errs.ErrValidation}, {"app,app", "", errs.ErrValidation},
		{"app", "app-linux-amd64", errs.ErrValidation},
		{"app,app-linux-amd64", "app-linux-amd64", errs.ErrValidation},
	} {
		t.Run(tc.names+"/"+tc.collision, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			dir := filepath.Join(root, "bin")
			require.NoError(t, os.Mkdir(dir, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "app"), []byte("binary"), 0o700)) //nolint:gosec // preserves executable fixture mode; never executed.
			require.NoError(t, os.WriteFile(filepath.Join(root, "outside"), []byte("canary"), 0o600))

			if tc.collision != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, tc.collision), []byte("keep"), 0o600))
			}

			before := snapshotContainerTree(t, root)

			var log bytes.Buffer

			names := strings.Replace(tc.names, "app,/outside", "app,"+filepath.Join(root, "outside"), 1)
			err := appcontainer.SuffixExtractedBinaries(&log, appcontainer.SuffixExtractedBinariesInput{Dir: dir, Arch: "amd64", ExpectedNames: names})
			require.ErrorIs(t, err, tc.want)

			if errors.Is(tc.want, errs.ErrMissingInput) {
				require.Contains(t, err.Error(), "missing, also-missing")
			}

			require.Empty(t, log.String())
			require.Equal(t, before, snapshotContainerTree(t, root))
		})
	}
}

func TestWriteDigestMarker_PreservesExistingEntries(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"link", "directory", "nonempty", "empty"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			dir := filepath.Join(root, "digests")
			require.NoError(t, os.Mkdir(dir, 0o700))

			canary := filepath.Join(root, "canary")
			require.NoError(t, os.WriteFile(canary, []byte("keep"), 0o600))

			marker := filepath.Join(dir, strings.TrimPrefix(oneDigest, "sha256:"))

			switch kind {
			case "link":
				require.NoError(t, os.Symlink(canary, marker))
			case "directory":
				require.NoError(t, os.Mkdir(marker, 0o700))
			case "nonempty":
				require.NoError(t, os.WriteFile(marker, []byte("keep"), 0o600))
			case "empty":
				require.NoError(t, os.WriteFile(marker, nil, 0o644)) //nolint:gosec // public empty digest-marker fixture.
			}

			before := snapshotContainerTree(t, root)

			path, err := appcontainer.WriteDigestMarker(appcontainer.WriteDigestMarkerInput{Digest: oneDigest, DigestsDir: dir})
			if kind == "empty" {
				require.NoError(t, err)
				require.Equal(t, marker, path)
			} else {
				require.ErrorIs(t, err, errs.ErrValidation)
				require.Empty(t, path)
			}

			require.Equal(t, before, snapshotContainerTree(t, root))
		})
	}
}

func TestInspectManifest_CanonicalDigestSelectors(t *testing.T) {
	t.Parallel()

	for _, ref := range []string{"registry.example/app:tag", "registry.example/app@" + oneDigest, "registry.example/app:tag@" + oneDigest} {
		sink := fakeoutputsink.New(t)
		registry := &fakeManifestRegistry{digest: oneDigest}
		_, err := appcontainer.InspectManifest(t.Context(), registry, sink, io.Discard, appcontainer.InspectManifestInput{Image: ref})
		require.NoError(t, err)
		require.Equal(t, "registry.example/app@"+oneDigest, sink.Single("image-digest-ref"))

		canonical := ref
		if strings.Contains(ref, "@") {
			canonical = "registry.example/app@" + oneDigest
		}

		require.Equal(t, []string{canonical}, registry.manifestCalls)
		require.Equal(t, []string{canonical}, registry.resolveCalls)
	}
}

func TestMergeManifest_PreflightsEveryDestination(t *testing.T) {
	t.Parallel()

	for _, later := range []string{"registry.example/app:bad tag", "registry.example/other:v2", "registry.example/app:v1", "registry.example/app@" + oneDigest} {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, strings.TrimPrefix(oneDigest, "sha256:")), nil, 0o644)) //nolint:gosec // public empty digest-marker fixture.

		registry := &fakeManifestRegistry{}

		var log bytes.Buffer

		err := appcontainer.MergeManifest(t.Context(), registry, &log, appcontainer.MergeManifestInput{ImageName: "registry.example/app", Tags: "registry.example/app:v1\n" + later, DigestsDir: dir})
		require.ErrorIs(t, err, errs.ErrValidation)
		require.Zero(t, registry.mergeCalls)
		require.Empty(t, log.String())
	}
}

type metadataPreflightProvider struct {
	provider.Provider
	calls int
}

func (p *metadataPreflightProvider) ResolveContext(context.Context) (*provider.EventContext, error) {
	p.calls++

	return &provider.EventContext{}, nil
}

func TestComputeMetadata_RejectsNonRepositoryNamesBeforeEffects(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"", " ", "https://registry.example/app", "registry.example/app:tag", "registry.example/app@" + oneDigest, "registry.example/Owner/app", "registry.example/a/../app", "app"} {
		prov := &metadataPreflightProvider{}
		sink := fakeoutputsink.New(t)

		_, err := appcontainer.ComputeMetadata(t.Context(), prov, nil, sink, nil, appcontainer.ComputeMetadataInput{ImageName: name, TagRules: "type=raw,value=test"})
		if name == "" {
			require.ErrorIs(t, err, errs.ErrUsage)
		} else {
			require.ErrorIs(t, err, errs.ErrValidation)
		}

		require.Zero(t, prov.calls)
		require.Empty(t, sink.Keys())
	}
}

// TestComputeMetadata_RejectsBadFlavorRulesAndSourceBeforeEffects extends the
// image-name refusals to every other input. Each row changes one field of a
// valid input; none may reach the provider or write an output. The source
// identity checks used to run after the event context had been resolved.
func TestComputeMetadata_RejectsBadFlavorRulesAndSourceBeforeEffects(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		mutate func(*appcontainer.ComputeMetadataInput)
		want   error
	}{
		"latest=true flavor":         {mutate: func(in *appcontainer.ComputeMetadataInput) { in.Flavor = "latest=true" }, want: errs.ErrUnsupported},
		"unsupported flavor":         {mutate: func(in *appcontainer.ComputeMetadataInput) { in.Flavor = "prefix=x" }, want: errs.ErrUnsupported},
		"unknown flavor":             {mutate: func(in *appcontainer.ComputeMetadataInput) { in.Flavor = "colour=blue" }, want: errs.ErrUsage},
		"unknown rule type":          {mutate: func(in *appcontainer.ComputeMetadataInput) { in.TagRules = "type=nope,value=x" }, want: errs.ErrUsage},
		"multi-line source ref":      {mutate: func(in *appcontainer.ComputeMetadataInput) { in.SourceRefName = "v1\nv2" }, want: errs.ErrUsage},
		"unknown source ref type":    {mutate: func(in *appcontainer.ComputeMetadataInput) { in.SourceRefType = "release" }, want: errs.ErrUsage},
		"multi-line source revision": {mutate: func(in *appcontainer.ComputeMetadataInput) { in.SourceRevision = "abc\r\n" }, want: errs.ErrUsage},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			in := appcontainer.ComputeMetadataInput{ImageName: "registry.example/app", TagRules: "type=raw,value=test"}
			tc.mutate(&in)

			prov := &metadataPreflightProvider{}
			sink := fakeoutputsink.New(t)

			_, err := appcontainer.ComputeMetadata(t.Context(), prov, nil, sink, nil, in)
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, prov.calls, "the provider was asked before the input was refused")
			require.Empty(t, sink.Keys())
		})
	}
}
