// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	apppublish "github.com/diggsweden/reusable-ci/v3/internal/app/publish"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/publish"
)

func TestRegistryAuth_OK(t *testing.T) {
	t.Parallel()

	var out, stderr bytes.Buffer

	err := apppublish.RegistryAuth(context.Background(), &out, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), publish.RegistryAuthInput{
		UseCIToken: true,
		Registry:   "ghcr.io", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})
	require.NoError(t, err)
	require.Contains(t, out.String(), "✓ Registry authentication")
}

func TestRegistryAuth_ErrorWhenNoPasswordAndCustomAuth(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	err := apppublish.RegistryAuth(context.Background(), &bytes.Buffer{}, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), publish.RegistryAuthInput{
		UseCIToken:  false,
		Registry:    "ghcr.io",
		HasPassword: false,
	})
	require.Error(t, err)
	require.Contains(t, stderr.String(), "::error::registry-password")
}

func TestRegistryAuth_WarningsOnCITokenWithCustomRegistry(t *testing.T) {
	t.Parallel()

	var out, stderr bytes.Buffer

	err := apppublish.RegistryAuth(context.Background(), &out, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), publish.RegistryAuthInput{
		UseCIToken:       true,
		Registry:         "https://npm.pkg.github.com",
		ExpectedRegistry: "ghcr.io",
	})
	require.NoError(t, err)
	require.Contains(t, stderr.String(), "::warning::Using CI token")
	require.Contains(t, out.String(), "✓ Registry authentication")
}
