// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package ociregistry

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/require"
)

// buildKitSource is one seeded source index and the child descriptors it
// published, as the merge must carry them.
type buildKitSource struct {
	indexHex    string
	children    []v1.Descriptor
	imageDigest v1.Hash
}

// seedRealisticBuildKitIndex writes what `buildx build --provenance --sbom`
// pushes for one platform: an OCI index holding an OCI image manifest with a
// platform variant, and an OCI attestation manifest whose in-toto layer
// carries a predicate-type annotation and whose descriptor points back at the
// image through vnd.docker.reference.digest.
func seedRealisticBuildKitIndex(t *testing.T, repo, arch, variant string) buildKitSource {
	t.Helper()

	img, err := random.Image(256, 1)
	require.NoError(t, err)

	cfg, err := img.ConfigFile()
	require.NoError(t, err)

	cfg = cfg.DeepCopy()
	cfg.OS, cfg.Architecture, cfg.Variant = osLinux, arch, variant

	img, err = mutate.ConfigFile(img, cfg)
	require.NoError(t, err)

	img = mutate.ConfigMediaType(mutate.MediaType(img, types.OCIManifestSchema1), types.OCIConfigJSON)

	imageDigest, err := img.Digest()
	require.NoError(t, err)

	attestation, err := mutate.Append(mutate.ConfigMediaType(mutate.MediaType(empty.Image, types.OCIManifestSchema1), types.OCIConfigJSON), mutate.Addendum{
		Layer:       static.NewLayer([]byte(`{"_type":"https://in-toto.io/Statement/v0.1","predicateType":"https://slsa.dev/provenance/v0.2"}`), "application/vnd.in-toto+json"),
		Annotations: map[string]string{"in-toto.io/predicate-type": "https://slsa.dev/provenance/v0.2"},
	})
	require.NoError(t, err)

	index := mutate.AppendManifests(mutate.IndexMediaType(empty.Index, types.OCIImageIndex),
		mutate.IndexAddendum{Add: img, Descriptor: v1.Descriptor{
			MediaType: types.OCIManifestSchema1, Platform: &v1.Platform{OS: osLinux, Architecture: arch, Variant: variant},
		}},
		mutate.IndexAddendum{Add: attestation, Descriptor: v1.Descriptor{
			MediaType: types.OCIManifestSchema1, Platform: &v1.Platform{OS: unknownPlatform, Architecture: unknownPlatform},
			Annotations: map[string]string{
				"vnd.docker.reference.type":   "attestation-manifest",
				"vnd.docker.reference.digest": imageDigest.String(),
			},
		}},
	)

	ref, err := name.ParseReference(repo+":src-"+arch, name.Insecure)
	require.NoError(t, err)
	require.NoError(t, remote.WriteIndex(ref, index, remote.WithContext(t.Context())))

	manifest, err := index.IndexManifest()
	require.NoError(t, err)

	digest, err := index.Digest()
	require.NoError(t, err)

	return buildKitSource{indexHex: digest.Hex, children: manifest.Manifests, imageDigest: imageDigest}
}

// TestMergeManifest_PreservesRealisticBuildKitDescriptors merges two
// realistic BuildKit sources and compares every merged child descriptor with
// the one its source published: digest, size, media type, platform including
// the variant, and annotations, so each attestation still names an image
// digest that is present in the merged index. The merged document is an OCI
// index, and every child is fetchable from it by digest. The older test used
// Docker media types and random attestation images, which carry none of
// these fields.
func TestMergeManifest_PreservesRealisticBuildKitDescriptors(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)

	repo := strings.TrimPrefix(srv.URL, "http://") + "/o/r"
	sources := []buildKitSource{
		seedRealisticBuildKitIndex(t, repo, "amd64", ""),
		seedRealisticBuildKitIndex(t, repo, "arm64", "v8"),
	}

	tag := repo + ":multi"
	require.NoError(t, New().MergeManifest(t.Context(), repo, []string{sources[0].indexHex, sources[1].indexHex}, []string{tag}))

	tagRef, err := name.ParseReference(tag, name.Insecure)
	require.NoError(t, err)

	merged, err := remote.Index(tagRef, remote.WithContext(t.Context()))
	require.NoError(t, err)

	mediaType, err := merged.MediaType()
	require.NoError(t, err)
	require.Equal(t, types.OCIImageIndex, mediaType)

	manifest, err := merged.IndexManifest()
	require.NoError(t, err)

	want := append(append([]v1.Descriptor{}, sources[0].children...), sources[1].children...)
	require.Equal(t, want, manifest.Manifests)

	present := map[string]bool{}
	for _, child := range manifest.Manifests {
		present[child.Digest.String()] = true

		fetched, fetchErr := remote.Get(tagRef.Context().Digest(child.Digest.String()), remote.WithContext(t.Context()))
		require.NoError(t, fetchErr)
		require.Equal(t, child.MediaType, fetched.MediaType)
	}

	for _, source := range sources {
		require.True(t, present[source.imageDigest.String()])
		require.Equal(t, source.imageDigest.String(), source.children[1].Annotations["vnd.docker.reference.digest"])
	}
}
