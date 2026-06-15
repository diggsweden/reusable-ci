// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package projecttype_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
)

func TestType_StringRendersUnderlying(t *testing.T) {
	t.Parallel()
	require.Equal(t, "maven", projecttype.Maven.String())
	require.Equal(t, "gradle-android", projecttype.GradleAndroid.String())
}

func TestDetectFromEntries_PriorityOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries []string
		want    projecttype.Type
	}{
		{"pom_wins", []string{"pom.xml", "package.json"}, projecttype.Maven},        //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{"package_json", []string{"package.json", "build.gradle"}, projecttype.NPM}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{"build_gradle_jvm", []string{"build.gradle"}, projecttype.Gradle},
		{"build_gradle_kts", []string{"build.gradle.kts"}, projecttype.Gradle},
		{"go_mod", []string{"go.mod"}, projecttype.Go},
		{"cargo", []string{"Cargo.toml"}, projecttype.Cargo},
		{"pyproject", []string{"pyproject.toml"}, projecttype.Python},
		{"requirements", []string{"requirements.txt"}, projecttype.Python},
		{"setup_py", []string{"setup.py"}, projecttype.Python},
		{"empty", []string{}, projecttype.Unknown},
		{"unknown_entries", []string{"README.md"}, projecttype.Unknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, projecttype.DetectFromEntries(tc.entries))
		})
	}
}

func TestIsIn_MembershipCheck(t *testing.T) {
	t.Parallel()

	list := []projecttype.Type{projecttype.Maven, projecttype.NPM, projecttype.Go}
	require.True(t, projecttype.IsIn(projecttype.Maven, list))
	require.True(t, projecttype.IsIn(projecttype.Go, list))
	require.False(t, projecttype.IsIn(projecttype.Cargo, list))
}

func TestUnknownTypeError_FormatsValidList(t *testing.T) {
	t.Parallel()

	err := &projecttype.UnknownTypeError{
		Input: "rust",
		Valid: []projecttype.Type{projecttype.Maven, projecttype.Cargo},
	}
	require.Contains(t, err.Error(), `"rust"`)
	require.Contains(t, err.Error(), "maven, cargo")
}

func TestUnknownTypeError_IsErrorInterface(t *testing.T) {
	t.Parallel()

	var (
		e     error = &projecttype.UnknownTypeError{Input: "x"}
		typed *projecttype.UnknownTypeError
	)

	require.ErrorAs(t, e, &typed)
}
