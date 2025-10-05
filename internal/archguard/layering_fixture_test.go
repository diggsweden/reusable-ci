// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archguard

import (
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The layering guard walks a tree that is currently clean, so passing proves
// only that: it says nothing about whether the guard would notice a violation.
// A classifier that returned the same layer for everything, or a rule table
// that listed no forbidden edge, would pass exactly as loudly. These fixtures
// are the difference between a guard and a green tick.

// TestLayerOf_ClassifiesEveryTreeIntoItsLayer pins the classifier ADR 0004's
// rules are stated in terms of. Everything under internal/ has to land
// somewhere, and the fallback is "utility", which is the strictest inward
// layer after domain, so a path that fell through by accident is refused
// rather than exempted.
func TestLayerOf_ClassifiesEveryTreeIntoItsLayer(t *testing.T) {
	t.Parallel()

	for path, want := range map[string]string{
		"domain":                "domain",
		"domain/pipeline":       "domain",
		"adapters":              "adapters",
		"adapters/gitlab":       "adapters",
		"app":                   "app",
		"app/build":             "app",
		"cli":                   "cli",
		"cli/commands/platform": "cli",
		"testutil":              "test",
		"testutil/toolrecorder": "test",
		"livetest":              "test",
		"livetest/conformance":  "test",
		"pathsafe":              "utility",
		"safeexec":              "utility",
		"listval":               "utility",
		"domainish":             "utility",
		"applesauce":            "utility",
		"clip":                  "utility",
		"adaptersomething":      "utility",
		"testutilities":         "utility",
	} {
		require.Equalf(t, want, layerOf(path), "layerOf(%q)", path)
	}
}

// TestForbiddenEdges_StateEveryOutwardArrow pins the rule table itself. Each
// layer names the layers it may not reach, and every named edge has a fix
// message, because a violation a contributor cannot act on is a guard that
// gets suppressed rather than obeyed.
func TestForbiddenEdges_StateEveryOutwardArrow(t *testing.T) {
	t.Parallel()

	forbidden := forbiddenEdges()
	require.Equal(t, map[string][]string{
		"domain":   {"app", "adapters", "cli"},
		"adapters": {"app", "cli"},
		"app":      {"adapters", "cli"},
		"utility":  {"app", "adapters", "cli"},
	}, forbidden, "the hexagon's outward arrows")

	for from, targets := range forbidden {
		for _, to := range targets {
			require.NotEmptyf(t, fixFor(from+"->"+to), "no fix guidance for %s->%s", from, to)
		}
	}

	// The inward edges the architecture exists to permit.
	for _, allowed := range [][2]string{{"cli", "app"}, {"cli", "domain"}, {"app", "domain"}, {"adapters", "domain"}, {"utility", "domain"}} {
		require.NotContainsf(t, forbidden[allowed[0]], allowed[1], "%s must be allowed to import %s", allowed[0], allowed[1])
	}
}

// TestViolationsIn_FindsOutwardImportsAndNothingElse drives the scanner over
// owned files. An import of another layer is only a violation when it points
// outward; the same file importing inward, importing a third-party package, or
// importing nothing at all must produce no finding.
func TestViolationsIn_FindsOutwardImportsAndNothingElse(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		from    string
		imports []string
		want    []string
	}{
		{
			name:    "a use case reaching for a concrete adapter",
			from:    "app",
			imports: []string{internalPrefix + "adapters/gitlab"},
			want:    []string{"app->adapters"},
		},
		{
			name:    "domain reaching outward in every direction",
			from:    "domain",
			imports: []string{internalPrefix + "app/build", internalPrefix + "adapters/git", internalPrefix + "cli/deps"},
			want:    []string{"domain->app", "domain->adapters", "domain->cli"},
		},
		{
			name:    "a utility constructing an adapter",
			from:    "utility",
			imports: []string{internalPrefix + "adapters/gpg"},
			want:    []string{"utility->adapters"},
		},
		{
			name:    "inward imports are the point of the architecture",
			from:    "app",
			imports: []string{internalPrefix + "domain/pipeline", internalPrefix + "pathsafe"},
		},
		{
			name:    "cli may reach everything it composes",
			from:    "cli",
			imports: []string{internalPrefix + "app/build", internalPrefix + "adapters/git", internalPrefix + "domain/build"},
		},
		{
			name:    "third-party imports are not layers",
			from:    "domain",
			imports: []string{"github.com/stretchr/testify/require", "os", "example.com/internal/app"},
		},
		{
			name: "a file with no imports",
			from: "domain",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "owned.go")
			require.NoError(t, os.WriteFile(path, []byte(goFileImporting(testCase.imports)), 0o600))

			found, err := violationsIn(token.NewFileSet(), path, testCase.from, forbiddenEdges())
			require.NoError(t, err)

			names := make([]string, 0, len(found))
			for _, edge := range found {
				names = append(names, edge.name)
				require.NotEmpty(t, edge.imported, "a violation must name the import that caused it")
			}

			require.Equal(t, testCase.want, nilIfEmpty(names))
		})
	}
}

// goFileImporting renders an owned source file with the given imports.
func goFileImporting(imports []string) string {
	if len(imports) == 0 {
		return "package owned\n"
	}

	var body strings.Builder
	body.WriteString("package owned\n\nimport (\n")

	for _, path := range imports {
		body.WriteString("\t\"" + path + "\"\n")
	}

	body.WriteString(")\n")

	return body.String()
}

func nilIfEmpty(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	return values
}
