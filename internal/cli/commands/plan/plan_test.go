// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package plan_test

import (
	"context"
	"strings"
	"testing"

	plancmd "github.com/diggsweden/reusable-ci/internal/cli/commands/plan"
	"github.com/diggsweden/reusable-ci/internal/testutil/ghaenv"
)

func TestWriteDevReleaseInterfaceCmd_ProjectTypePrecedence(t *testing.T) {
	tests := []struct {
		name                string
		projectType         string
		fallbackProjectType string
		want                string
	}{
		{name: "fallback_used", fallbackProjectType: "cargo", want: "cargo"},
		{name: "project_type_wins", projectType: "maven", fallbackProjectType: "cargo", want: "maven"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			env := ghaenv.Setup(t)
			env.Setenv("PROJECT_TYPE", testCase.projectType)
			env.Setenv("FALLBACK_PROJECT_TYPE", testCase.fallbackProjectType)
			env.Setenv("BRANCH", "main")

			cmd := plancmd.New()
			if err := cmd.Run(context.Background(), []string{"plan", "write-dev-release-interface"}); err != nil {
				t.Fatal(err)
			}
			if got := env.Output("dev-context-json"); !strings.Contains(got, `"project_type":"`+testCase.want+`"`) {
				t.Errorf("dev-context-json = %s", got)
			}
		})
	}
}

func TestGetFilePatternCmd_EnvMode(t *testing.T) {
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

			cmd := plancmd.New()
			if err := cmd.Run(context.Background(), []string{"plan", "get-file-pattern"}); err != nil {
				t.Fatal(err)
			}
			if got := env.Output("pattern"); got != testCase.want {
				t.Errorf("pattern = %q, want %q", got, testCase.want)
			}
		})
	}
}
