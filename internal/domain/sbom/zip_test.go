// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/domain/sbom"
)

func TestZipName_CanonicalShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		project string
		version string
		want    string
	}{
		{"simple", "my-app", "1.2.3", "my-app-1.2.3-sboms.zip"},
		{"scoped_npm", "@org/pkg", "0.1.0", "@org/pkg-0.1.0-sboms.zip"},
		{"prerelease_with_v_prefix", "lib", "v2.0.0-rc.1", "lib-v2.0.0-rc.1-sboms.zip"},
		{"empty_version_keeps_separator", "x", "", "x--sboms.zip"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, sbom.ZipName(testCase.project, testCase.version))
		})
	}
}
