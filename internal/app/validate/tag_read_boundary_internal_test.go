// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"bytes"
	"context"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/stretchr/testify/require"
	"testing"
)

type unreadableTag struct {
	gitOps
	cause error
}

func (r unreadableTag) CatFileType(context.Context, string) (string, error) { return "", r.cause }

func TestTagReadBoundary_DoesNotInventLightweightDiagnosis(t *testing.T) {
	t.Parallel()

	for _, cause := range []error{errs.ErrMissingInput, errs.ErrPermissionDenied} {
		var out bytes.Buffer

		err := TagSignature(t.Context(), unreadableTag{cause: cause}, &out, output.Annotator{}, TagSignatureInput{Tag: "v1.0.0"})
		require.ErrorIs(t, err, cause)
		require.NotContains(t, err.Error(), "lightweight")
		require.NotContains(t, out.String(), "lightweight")
	}
}
