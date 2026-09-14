// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/release"
	"github.com/stretchr/testify/require"
	"testing"
	"testing/fstest"
)

func TestWalkSafe_RejectsControlClass(t *testing.T) {
	t.Parallel()

	for _, control := range []rune{'\t', '\n', '\r', 1, 31, 127, 133} {
		require.ErrorIs(t, release.WalkSafe(fstest.MapFS{"file" + string(control): &fstest.MapFile{Data: []byte("fixture")}}), errs.ErrValidation)
	}

	require.NoError(t, release.WalkSafe(fstest.MapFS{"ordinary file": &fstest.MapFile{Data: []byte("fixture")}}))
}
