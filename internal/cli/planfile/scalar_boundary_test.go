// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package planfile_test

import (
	"github.com/diggsweden/reusable-ci/v3/internal/cli/planfile"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestPlanScalarBoundary_PreservesJSONSpelling(t *testing.T) {
	testenv.New(t)

	for _, value := range []string{"9007199254740993", "-9007199254740993", "1.2300", "1.20e+30", "true", "false"} {
		path := filepath.Join(t.TempDir(), "plan.json")
		require.NoError(t, os.WriteFile(path, []byte(`{"scope":{"value":`+value+`}}`), 0o600))
		t.Setenv(planfile.EnvVar, path)

		chain := planfile.Vars("scope", "value")
		got, ok := chain.Lookup()
		require.True(t, ok)
		require.Equal(t, value, got)
	}
}
