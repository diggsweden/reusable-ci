// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package ociregistry

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/stretchr/testify/require"
)

func TestLayoutImage_RequiresExactlyOneImage(t *testing.T) {
	t.Parallel()

	for _, count := range []int{0, 1, 2} {
		index := v1.ImageIndex(empty.Index)
		for range count {
			index = mutate.AppendManifests(index, mutate.IndexAddendum{Add: empty.Image})
		}

		dir := t.TempDir()
		_, err := layout.Write(dir, index)
		require.NoError(t, err)

		image, err := layoutImage(dir)
		if count == 1 {
			require.NoError(t, err)
			require.NotNil(t, image)
		} else {
			require.ErrorIs(t, err, errs.ErrMalformedInput)
			require.Nil(t, image)
		}
	}
}

func TestMergedIndex_RejectsDuplicateConcretePlatforms(t *testing.T) {
	t.Parallel()

	for _, duplicate := range []bool{false, true} {
		second := "arm64"
		if duplicate {
			second = "amd64"
		}

		index := mutate.AppendManifests(empty.Index,
			mutate.IndexAddendum{Add: empty.Image, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "amd64"}}},
			mutate.IndexAddendum{Add: empty.Image, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: second}}},
			mutate.IndexAddendum{Add: empty.Image, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "unknown", Architecture: "unknown"}}},
			mutate.IndexAddendum{Add: empty.Image, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "unknown", Architecture: "unknown"}}})

		err := validateIndexPlatforms(index)
		if duplicate {
			require.ErrorIs(t, err, errs.ErrValidation)
		} else {
			require.NoError(t, err)
		}
	}
}

func TestLinuxSelection_RejectsPartialPlatformClaims(t *testing.T) {
	t.Parallel()

	partial := v1.Descriptor{Platform: &v1.Platform{Architecture: "amd64"}}
	noArch := v1.Descriptor{Platform: &v1.Platform{OS: "linux"}}
	_, err := findLinuxArchDescriptor(&v1.IndexManifest{Manifests: []v1.Descriptor{partial}}, "fixture", "amd64")
	require.ErrorIs(t, err, errs.ErrMissingInput)

	linux := v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "amd64"}, Size: 42}
	got, err := findLinuxArchDescriptor(&v1.IndexManifest{Manifests: []v1.Descriptor{partial, noArch, linux}}, "fixture", "amd64")
	require.NoError(t, err)
	require.Equal(t, int64(42), got.Size)
}

// TestMergedIndex_PlatformVariantsAreDistinct: linux/arm/v6 and linux/arm/v7
// are two platforms an index may carry side by side, so the duplicate check
// keys on the variant as well; two v7 children are still a duplicate.
func TestMergedIndex_PlatformVariantsAreDistinct(t *testing.T) {
	t.Parallel()

	arm := func(variant string) mutate.IndexAddendum {
		return mutate.IndexAddendum{Add: empty.Image, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "arm", Variant: variant}}}
	}

	require.NoError(t, validateIndexPlatforms(mutate.AppendManifests(empty.Index, arm("v6"), arm("v7"))))
	require.ErrorIs(t, validateIndexPlatforms(mutate.AppendManifests(empty.Index, arm("v7"), arm("v7"))), errs.ErrValidation)
}
