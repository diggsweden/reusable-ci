// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/validate"
)

func TestRequireTagRefType_EmptyTypeUsage(t *testing.T) {
	t.Parallel()

	err := validate.RequireTagRefType("", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "usage")
}

func TestRequireTagRefType_TagSucceeds(t *testing.T) {
	t.Parallel()
	require.NoError(t, validate.RequireTagRefType(provider.RefTypeTag, "refs/tags/v1.0.0"))
}

func TestRequireTagRefType_BranchFails(t *testing.T) {
	t.Parallel()

	err := validate.RequireTagRefType(provider.RefTypeBranch, "refs/heads/main")
	require.Error(t, err)

	var rte *validate.RefTypeError
	require.ErrorAs(t, err, &rte)
	require.Equal(t, provider.RefTypeBranch, rte.Got)
	require.Equal(t, "refs/heads/main", rte.Ref)
	require.Contains(t, err.Error(), `got "branch"`)
	require.Contains(t, err.Error(), `ref "refs/heads/main"`)
}
