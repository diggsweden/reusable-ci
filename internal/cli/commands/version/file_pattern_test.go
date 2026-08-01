// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"context"
	"testing"

	versioncmd "github.com/diggsweden/reusable-ci/v3/internal/cli/commands/version"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/ghaenv"
)

func TestFilePatternCmd_EnvMode(t *testing.T) {
	tests := []struct {
		name            string
		projectType     string
		explicitPattern string
		want            string
	}{
		{name: "explicit_pattern", projectType: "maven", explicitPattern: "pom.xml package.json", want: "pom.xml package.json"},
		{name: "project_type_fallback", projectType: "npm", want: "CHANGELOG.md package.json package-lock.json"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			env := ghaenv.Setup(t)
			env.Setenv("PROJECT_TYPE", testCase.projectType)
			env.Setenv("EXPLICIT_FILE_PATTERN", testCase.explicitPattern)

			cmd := versioncmd.New()
			if err := cmd.Run(context.Background(), []string{"version", "file-pattern"}); err != nil {
				t.Fatal(err)
			}

			if got := env.Output("pattern"); got != testCase.want {
				t.Errorf("pattern = %q, want %q", got, testCase.want)
			}
		})
	}
}
