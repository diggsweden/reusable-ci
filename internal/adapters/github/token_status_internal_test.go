// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"errors"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestTokenStatusBoundary_DoesNotReclassifyOutages(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		status int
		want   error
	}{{200, nil}, {401, errs.ErrPermissionDenied}, {403, errs.ErrPermissionDenied}, {404, errs.ErrMissingInput}, {429, errs.ErrRateLimited}, {503, errs.ErrDependencyUnavailable}} {
		calls := 0
		p := &Provider{APIBaseOverride: "https://github.invalid", HTTPClient: &http.Client{Transport: contractTransport(func(req *http.Request) (*http.Response, error) {
			calls++

			return contractResponse(req, tc.status, `{}`), nil
		})}}
		err := p.ValidateToken(t.Context(), "synthetic", "owner/repo")
		require.ErrorIs(t, err, tc.want)

		if !errors.Is(tc.want, errs.ErrPermissionDenied) {
			require.NotErrorIs(t, err, errs.ErrPermissionDenied)
		}

		require.Equal(t, 1, calls)
	}
}
