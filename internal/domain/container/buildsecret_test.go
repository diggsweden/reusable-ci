// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
)

func TestBuildRequest_ValidatesCompleteSecretGrammar(t *testing.T) {
	t.Parallel()

	for _, secret := range []string{"id=a", "src=fixture", "id=,src=fixture", "id=a,src=", "id=a,src= ", "id=a,id=b,src=fixture", "id=a,src=one,src=two", "id=a,src=fixture,type=env", "id=a,src=fixture,broken", "id=../a,src=fixture", "id=a,src=fixture\nother"} {
		request := container.BuildRequest{Context: ".", Mode: container.BuildModeLocal, OutputDir: "fixture-output", Secrets: []string{secret}}
		require.ErrorIs(t, request.Validate(), errs.ErrUsage, "%s", secret)
	}

	for _, secret := range []string{"id=a,src=fixture", "src=fixture,id=a", "id=a,src=fixture=with-equals"} {
		for _, mode := range []container.BuildOutputMode{container.BuildModeLoad, container.BuildModeLocal, container.BuildModePushByDigest} {
			request := container.BuildRequest{Context: ".", Mode: mode, ImageRef: "registry.example/app", OutputDir: "fixture-output", Secrets: []string{secret}}
			require.NoError(t, request.Validate())
		}
	}
}
