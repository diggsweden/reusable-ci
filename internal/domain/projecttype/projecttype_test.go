// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package projecttype_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
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

// TestDetectFromEntries_EveryPriorityTransition covers each adjacent pair in
// the ladder, not the two that happened to get cases.
//
// The existing table pins Maven-over-npm and npm-over-Gradle and stops there,
// so the Gradle/Go, Go/Cargo and Cargo/Python boundaries are decided by nothing
// — moving Cargo ahead of Go, or Python ahead of Cargo, leaves every case
// passing. Priority is the whole content of this function: a repository with
// both a go.mod and a Cargo.toml is common (a Rust tool with a Go wrapper, or
// the reverse), and classifying it wrong selects the wrong build, the wrong
// SBOM generator and the wrong release artifacts.
//
// Each pair is also asserted in both input orders. Detection builds a set, so
// order cannot matter today — which is exactly why it is worth pinning before
// someone replaces the set with a loop over entries.
func TestDetectFromEntries_EveryPriorityTransition(t *testing.T) {
	t.Parallel()

	ladder := []struct {
		manifest string
		want     projecttype.Type
	}{
		{"pom.xml", projecttype.Maven},
		{"package.json", projecttype.NPM},
		{"build.gradle", projecttype.Gradle},
		{"go.mod", projecttype.Go},
		{"Cargo.toml", projecttype.Cargo},
		{"pyproject.toml", projecttype.Python},
	}

	for i := range len(ladder) - 1 {
		higher, lower := ladder[i], ladder[i+1]

		t.Run(higher.manifest+" beats "+lower.manifest, func(t *testing.T) {
			t.Parallel()

			for _, entries := range [][]string{
				{higher.manifest, lower.manifest},
				{lower.manifest, higher.manifest},
			} {
				if got := projecttype.DetectFromEntries(entries); got != higher.want {
					t.Errorf("DetectFromEntries(%v) = %v, want %v", entries, got, higher.want)
				}
			}
		})
	}
}

// TestDetectFromEntries_EveryManifestAtOnce is the transitive control: pairwise
// ordering could be right at each step and still produce the wrong answer for a
// repository carrying all of them.
func TestDetectFromEntries_EveryManifestAtOnce(t *testing.T) {
	t.Parallel()

	all := []string{
		"pyproject.toml", "Cargo.toml", "go.mod",
		"build.gradle.kts", "package.json", "pom.xml",
	}

	if got := projecttype.DetectFromEntries(all); got != projecttype.Maven {
		t.Errorf("DetectFromEntries(%v) = %v, want %v", all, got, projecttype.Maven)
	}

	// Reversed, and with duplicates, so neither position in the slice nor a
	// repeated basename changes the answer.
	shuffled := []string{
		"pom.xml", "package.json", "package.json", "build.gradle.kts",
		"go.mod", "Cargo.toml", "pyproject.toml", "pom.xml",
	}

	if got := projecttype.DetectFromEntries(shuffled); got != projecttype.Maven {
		t.Errorf("DetectFromEntries(%v) = %v, want %v", shuffled, got, projecttype.Maven)
	}
}
