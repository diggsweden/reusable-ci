// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/stretchr/testify/require"
	"testing"
)

type previewMutationRecorder struct {
	provider.Provider
	calls int
}

func (r *previewMutationRecorder) CreateRelease(context.Context, string, provider.ReleaseSpec) error {
	r.calls++

	return nil
}
func (r *previewMutationRecorder) PublishRelease(context.Context, string, provider.ReleaseSpec) error {
	r.calls++

	return nil
}

func TestReleasePreviewBoundary_DoesNotDelegateMutations(t *testing.T) {
	t.Parallel()

	for _, preview := range []bool{true, false} {
		recorder := &previewMutationRecorder{}
		dep := &deps.Deps{Provider: recorder}
		creator, err := publishRecreateCreator(dep, preview)
		require.NoError(t, err)
		publisher, err := publishReconcilePublisher(dep, preview)
		require.NoError(t, err)

		spec := provider.ReleaseSpec{Tag: "v1.0.0"}
		require.NoError(t, creator.CreateRelease(t.Context(), "o/r", spec))
		require.NoError(t, publisher.PublishRelease(t.Context(), "o/r", spec))

		want := 2
		if preview {
			want = 0
		}

		require.Equal(t, want, recorder.calls)
	}
}
