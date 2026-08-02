// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package report

import (
	"context"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/ghaenv"
)

// kindCase drives one registry kind end-to-end through the real CLI
// command: minimal required flags in, rendered step-summary block out.
type kindCase struct {
	args   []string
	marker string // distinctive header the rendered block must contain
}

// kindCases must cover every kind in the registry — TestKindRegistry
// fails when a kind is added without a row here (and when a row goes
// stale against a removed kind).
func kindCases() map[string]kindCase {
	return map[string]kindCase{
		"build maven": {
			args:   []string{"--build-type", "library", "--group-id", "com.example", "--artifact-id", "app", "--version", "1.2.3", "--java-version", "21"},
			marker: "## Maven Build Summary",
		},
		"build npm": {
			args:   []string{"--package-name", "@org/app", "--version", "1.2.3", "--node-version", "22"},
			marker: "## NPM Build Summary",
		},
		"build gradle": {
			args:   []string{"--java-version", "21", "--tasks", "build"},
			marker: "## Gradle Build Summary",
		},
		"build android": {
			args:   []string{"--java-version", "21", "--jdk-dist", "temurin", "--build-module", "app"},
			marker: "## Android Variants Build Summary",
		},
		"build go": {
			args:   []string{"--binary-name", "app", "--module", "example.com/app", "--platforms", "linux/amd64", "--version", "1.2.3"},
			marker: "## Go Build Summary",
		},
		"build xcode": {
			args:   []string{"--xcode-version", "16.0", "--scheme", "App"},
			marker: "## Xcode Build Summary",
		},
		"publish appstore": {
			args:   []string{"--ipa-file", "App.ipa", "--platform", "ios"},
			marker: "## App Store Connect Upload Summary",
		},
		"publish google-play": {
			args:   []string{"--aab-file", "app.aab", "--package-name", "com.example.app", "--track", "internal", "--status", "completed"},
			marker: "## Google Play Upload Summary",
		},
		"publish maven-central": {
			args:   []string{"--version", "1.2.3"},
			marker: "## Published to Maven Central",
		},
		"publish forge-packages": {
			args:   []string{"--repository", "org/app", "--package-type", "maven", "--registry-name", "GitHub Packages"},
			marker: "## Published to GitHub Packages",
		},
	}
}

// runReport runs the real `report ...` command tree with the given
// subcommand argv.
func runReport(t *testing.T, argv ...string) error {
	t.Helper()

	return New().Run(context.Background(), append([]string{"report"}, argv...))
}

// TestKindRegistry_EveryKindRenders runs each registry kind through the
// real `report <group> <kind>` command with its minimal required flags
// and asserts the kind's summary block lands on the step-summary sink.
func TestKindRegistry_EveryKindRenders(t *testing.T) {
	groups := map[string][]summaryKind{
		"build":   buildKinds(),
		"publish": publishKinds(),
	}

	cases := kindCases()
	seen := make(map[string]bool, len(cases))

	for group, kinds := range groups {
		for _, k := range kinds {
			key := group + " " + k.name

			seen[key] = true

			tc, ok := cases[key]
			if !ok {
				t.Errorf("registry kind %q has no test case — add a row to kindCases", key)

				continue
			}

			t.Run(key, func(t *testing.T) {
				env := ghaenv.Setup(t)

				argv := append([]string{group, k.name}, tc.args...)
				if err := runReport(t, argv...); err != nil {
					t.Fatalf("run %q: %v", key, err)
				}

				body := env.Summary()
				if strings.TrimSpace(body) == "" {
					t.Fatalf("kind %q rendered an empty step summary", key)
				}

				if !strings.Contains(body, tc.marker) {
					t.Errorf("kind %q: summary missing %q in %s", key, tc.marker, body)
				}
			})
		}
	}

	for key := range cases {
		if !seen[key] {
			t.Errorf("test case %q does not match any registry kind — remove or fix the row", key)
		}
	}
}

// TestKindRegistry_RequiredFlagsAreEnforced asserts the shared
// scaffolding rejects a missing required flag for every kind that
// declares one.
func TestKindRegistry_RequiredFlagsAreEnforced(t *testing.T) {
	groups := map[string][]summaryKind{
		"build":   buildKinds(),
		"publish": publishKinds(),
	}

	for group, kinds := range groups {
		for _, k := range kinds {
			if len(k.required) == 0 {
				continue
			}

			t.Run(group+" "+k.name, func(t *testing.T) {
				ghaenv.Setup(t)

				err := runReport(t, group, k.name)
				if err == nil {
					t.Fatalf("kind %q: expected required-flag error, got nil", k.name)
				}

				if !strings.Contains(err.Error(), "--"+k.required[0]) {
					t.Errorf("kind %q: error %q does not name the missing flag", k.name, err)
				}
			})
		}
	}
}
