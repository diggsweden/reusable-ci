// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"encoding/json"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPushReferenceBoundary_RejectsUnsafeLocalNames(t *testing.T) {
	t.Parallel()

	for _, ref := range []string{"bad ref", "localhost/app\n--flag", "https://registry.example/app", "localhost/a/../app"} {
		raw := []byte(`{"schemaVersion":2}`)
		tool := &fakeImagePushTool{digest: manifestTestDigest(raw)}
		_, err := appcontainer.PushImage(t.Context(), tool, &fakeManifestPushRegistry{raw: raw}, nil, io.Discard, appcontainer.PushImageInput{LocalImage: ref, Destination: "registry.example/app:v1", TLSVerify: "true", RetryAttempts: 1})
		require.ErrorIs(t, err, errs.ErrUsage)
		require.Empty(t, tool.pushes)
	}
}

func TestPushReferenceBoundary_ProducesOneDigestSuffix(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"schemaVersion":2}`)
	digest := manifestTestDigest(raw)
	tool := &fakeManifestPushTool{digest: digest}
	got, err := appcontainer.PushManifest(t.Context(), tool, &fakeManifestPushRegistry{raw: raw}, nil, io.Discard, appcontainer.PushManifestInput{LocalManifest: "localhost/app:v1", Destination: "registry.example/app@" + digest, TLSVerify: "true"})
	require.NoError(t, err)
	require.Equal(t, "registry.example/app@"+digest, got.Ref)
}

func TestLogoutBoundary_ValidatesBeforeCreatingState(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	err := appcontainer.RegistryLogout(io.Discard, appcontainer.RegistryLogoutInput{AuthFile: filepath.Join(root, "missing", "auth.json")})
	require.ErrorIs(t, err, errs.ErrUsage)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestSignerMetadataBoundary_PreflightsAllArchitectureBindings(t *testing.T) {
	for _, kind := range []string{"duplicate-digest", "duplicate-arch", "arch", "digest", "tag"} {
		t.Run(kind, func(t *testing.T) {
			t.Chdir(t.TempDir())

			const repo = "codeberg.org/itiquette/forgejo-ci-signer"

			writeAuthFileForSignerImage(t, "auth.json")
			writeSignerArchMetadata(t, "amd64", repo, strings.Repeat("1", 64))
			writeSignerArchMetadata(t, "arm64", repo, strings.Repeat("2", 64))

			path := "signer-image-arch-arm64/signer-image-arm64.json"

			var meta appcontainer.SignerImageArchMetadata
			readJSONForSignerImage(t, path, &meta)

			archs := []string{"amd64", "arm64"}

			switch kind {
			case "duplicate-digest":
				meta.Digest = "sha256:" + strings.Repeat("1", 64)
				meta.Ref = repo + "@" + meta.Digest
			case "duplicate-arch":
				archs = []string{"amd64", "amd64"}
			case "arch":
				meta.Arch = "amd64"
			case "digest":
				meta.Digest = "sha256:" + strings.Repeat("3", 64)
			case "tag":
				meta.Tag = repo + ":signer-stale-arm64"
			}

			body, err := json.Marshal(meta)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, body, 0o600))
			require.NoError(t, os.Mkdir("signer-image-dist", 0o700))
			require.NoError(t, os.WriteFile("signer-image-dist/canary", []byte("old"), 0o600))
			before := snapshotContainerTree(t, ".")
			tool := &fakeSignerImageTool{}
			_, err = appcontainer.AssembleSignerImageManifest(t.Context(), tool, nil, nil, io.Discard, appcontainer.SignerImageAssembleInput{AuthFile: "auth.json", SourceSHA: strings.Repeat("b", 40), ServerURL: "https://codeberg.org", Repository: "itiquette/forgejo-ci", RepositorySuffix: "-signer", TagPrefix: "signer-", Name: "signer-image", Archs: archs})
			require.ErrorIs(t, err, errs.ErrValidation)
			require.Empty(t, tool.removedManifest)
			require.Empty(t, tool.createdManifest)
			require.Empty(t, tool.adds)
			require.Equal(t, before, snapshotContainerTree(t, "."))
		})
	}
}

func TestReleaseImagesBoundary_RejectsLinkedPathComponents(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"root", "parent", "leaf"} {
		root := t.TempDir()
		dist, outside := filepath.Join(root, "dist"), filepath.Join(root, "outside")
		require.NoError(t, os.Mkdir(outside, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(outside, "ledger.json"), []byte("canary"), 0o600))

		path := dist + "/ledger.json"
		if kind == "root" {
			require.NoError(t, os.Symlink(outside, dist))
		} else {
			require.NoError(t, os.Mkdir(dist, 0o700))

			if kind == "parent" {
				require.NoError(t, os.Symlink(outside, dist+"/linked"))
				path = dist + "/linked/ledger.json"
			} else {
				require.NoError(t, os.Symlink(outside+"/ledger.json", path))
			}
		}

		require.ErrorIs(t, appcontainer.ValidateReleaseImagesPath(path, dist), errs.ErrValidation)
	}
}
