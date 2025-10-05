// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package planfile_test

import (
	"github.com/diggsweden/reusable-ci/v3/internal/cli/planfile"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestPlanDefaultBoundary_IgnoresAmbientInputs(t *testing.T) {
	writePlan(t, `{"container build":{"context":"ambient-plan"}}`)
	t.Setenv("BUILD_CONTEXT", "ambient-env")

	path := os.Getenv(planfile.EnvVar)

	t.Run("precedence", TestPlanPrecedence_FlagBeatsPlanBeatsEnvBeatsDefault)
	require.Equal(t, path, os.Getenv(planfile.EnvVar))
	require.Equal(t, "ambient-env", os.Getenv("BUILD_CONTEXT"))
}
