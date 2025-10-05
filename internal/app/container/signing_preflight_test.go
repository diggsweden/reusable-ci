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
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
	"github.com/stretchr/testify/require"
)

func TestSignLedgerImages_PreparesEveryEntryBeforePublication(t *testing.T) {
	for _, kind := range []string{"invalid entry", "bad later pin", "reserved provenance", "duplicate SBOM"} {
		t.Run(kind, func(t *testing.T) {
			t.Chdir(t.TempDir())
			first := ledgerSignEntry(t)
			second := first
			second.SBOM = "dist/second.cyclonedx.json"

			require.NoError(t, os.Mkdir("dist", 0o700))
			require.NoError(t, os.WriteFile(second.SBOM, []byte("fixture"), 0o600))

			switch kind {
			case "invalid entry":
				second.Role = ""
			case "bad later pin":
				second.SBOMSHA256 = strings.Repeat("a", 64)
			case "reserved provenance":
				second.Provenance = map[string]any{"image": "override"}
			case "duplicate SBOM":
				second.SBOM = first.SBOM
			}

			signer := &recordingImageSigner{}

			var log bytes.Buffer

			err := appcontainer.SignLedgerImages(t.Context(), signer, &fakeLedgerSyft{}, fakeLedgerResolver{first.CandidateTag: ledgerSignDigest}, &log, io.Discard, appcontainer.SignLedgerImagesInput{Entries: []imageledger.Entry{first, second}, ReleaseTag: "v1.2.3", PredicatePath: writeBasePredicate(t), Method: domainrelease.SignMethodSigstore})
			require.ErrorIs(t, err, errs.ErrValidation)
			require.Empty(t, signer.signs)
			require.Empty(t, signer.attests)
			require.Empty(t, log.String())
		})
	}
}

func TestSignLedgerImages_PreparedEntriesPublishInOrder(t *testing.T) {
	t.Chdir(t.TempDir())
	first := ledgerSignEntry(t)
	second := first
	second.Ref = "codeberg.org/itiquette/second@" + ledgerSignDigest
	second.FinalTag = "codeberg.org/itiquette/second:v1.2.3-rust"
	second.MovingTag = "codeberg.org/itiquette/second:rust"
	second.CandidateTag = "codeberg.org/itiquette/second:staging-v1.2.3-rust"
	second.SBOM = "dist/second.cyclonedx.json"
	syft := &fakeLedgerSyft{}
	signer := &recordingImageSigner{beforeSign: func() { require.Len(t, syft.targets, 2, "both SBOMs must be prepared before any signature") }}
	err := appcontainer.SignLedgerImages(t.Context(), signer, syft, fakeLedgerResolver{first.CandidateTag: ledgerSignDigest, second.CandidateTag: ledgerSignDigest}, io.Discard, io.Discard, appcontainer.SignLedgerImagesInput{Entries: []imageledger.Entry{first, second}, ReleaseTag: "v1.2.3", PredicatePath: writeBasePredicate(t), Method: domainrelease.SignMethodSigstore})
	require.NoError(t, err)
	require.Len(t, signer.signs, 2)
	require.Len(t, signer.attests, 4)

	for index, entry := range []imageledger.Entry{first, second} {
		want := entry.CandidateTag + "@" + ledgerSignDigest
		require.Equal(t, want, signer.signs[index].ImageRef)
		require.Equal(t, want, signer.attests[2*index].input.ImageRef)
		require.Equal(t, want, signer.attests[2*index+1].input.ImageRef)
	}
}

func TestSignLedgerImages_ConfinesSBOMPathsBeforeTools(t *testing.T) {
	for _, kind := range []string{"traversal", "leaf link", "parent link"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			workspace := filepath.Join(root, "workspace")
			outside := filepath.Join(root, "outside")

			require.NoError(t, os.Mkdir(workspace, 0o700))
			require.NoError(t, os.Mkdir(outside, 0o700))
			t.Chdir(workspace)

			canary := filepath.Join(outside, "sbom.cyclonedx.json")
			require.NoError(t, os.WriteFile(canary, []byte("canary"), 0o600))
			entry := ledgerSignEntry(t)

			switch kind {
			case "traversal":
				entry.SBOM = "../outside/sbom.cyclonedx.json"
			case "leaf link":
				entry.SBOM = "sbom.cyclonedx.json"
				require.NoError(t, os.Symlink(canary, entry.SBOM))
			case "parent link":
				require.NoError(t, os.Symlink(outside, "linked"))

				entry.SBOM = "linked/sbom.cyclonedx.json"
			}

			predicate := writeBasePredicate(t)
			before := snapshotContainerTree(t, root)
			signer, syft := &recordingImageSigner{}, &fakeLedgerSyft{}
			err := appcontainer.SignLedgerImages(t.Context(), signer, syft, fakeLedgerResolver{entry.CandidateTag: ledgerSignDigest}, io.Discard, io.Discard, appcontainer.SignLedgerImagesInput{Entries: []imageledger.Entry{entry}, ReleaseTag: "v1.2.3", PredicatePath: predicate, Method: domainrelease.SignMethodSigstore})
			require.ErrorIs(t, err, errs.ErrValidation)
			require.Empty(t, syft.targets)
			require.Empty(t, signer.signs)
			require.Empty(t, signer.attests)
			require.Equal(t, before, snapshotContainerTree(t, root))
		})
	}
}

func TestSignerMetadataNames_RefuseBeforeToolsOrDirectoryChanges(t *testing.T) {
	for _, name := range []string{"../escape", ".", "..", "nested/name", "nested\\name"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			require.NoError(t, os.WriteFile("auth.json", []byte(`{"auths":{}}`), 0o600))
			require.NoError(t, os.Mkdir("metadata", 0o700))
			require.NoError(t, os.WriteFile("metadata/keep", []byte("keep"), 0o600))
			before := snapshotContainerTree(t, root)
			tool := &fakeSignerImageTool{}

			var log bytes.Buffer

			_, err := appcontainer.BuildSignerImageArch(t.Context(), tool, &log, appcontainer.SignerImageBuildArchInput{AuthFile: "auth.json", Arch: "amd64", SourceSHA: strings.Repeat("a", 40), ServerURL: "https://registry.example", Repository: "owner/app", Name: name, MetadataDir: "metadata"})
			require.ErrorIs(t, err, errs.ErrUsage)
			_, err = appcontainer.AssembleSignerImageManifest(t.Context(), tool, nil, nil, &log, appcontainer.SignerImageAssembleInput{AuthFile: "auth.json", SourceSHA: strings.Repeat("a", 40), ServerURL: "https://registry.example", Repository: "owner/app", Name: name, MetadataDir: "metadata"})
			require.ErrorIs(t, err, errs.ErrUsage)
			require.Empty(t, tool.buildReqs)
			require.Empty(t, tool.removedManifest)
			require.Empty(t, tool.createdManifest)
			require.Empty(t, log.String())
			require.Equal(t, before, snapshotContainerTree(t, root))
		})
	}
}

func TestSignImage_DoesNotLogKMSKeyReference(t *testing.T) {
	t.Parallel()

	const key = "awskms://synthetic-reference?token=private-reference-fixture"

	signer := &recordingSigner{}

	var log bytes.Buffer

	err := appcontainer.SignImage(t.Context(), signer, &log, appcontainer.SignImageInput{Image: "registry.example/app@" + oneDigest, Method: domainrelease.SignMethodKMS, KeyRef: key})
	require.NoError(t, err)
	require.Equal(t, key, signer.got.KeyRef)
	require.Contains(t, log.String(), "method=kms")
	require.NotContains(t, log.String(), key)
	require.NotContains(t, log.String(), "private-reference-fixture")
}
