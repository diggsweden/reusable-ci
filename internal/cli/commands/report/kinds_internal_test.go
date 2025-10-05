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

// TestKindRegistry_RequiredFlagsAreEnforced asserts that EVERY required flag
// of every kind is enforced individually, not just the first one.
//
// It used to run each kind with no flags at all and check the error named
// k.required[0]. That proved only that the loop fires once: with ten kinds
// declaring up to five required flags each, roughly two thirds of them were
// unverified, and an enforcement loop that stopped after the first entry
// would have stayed green.
//
// Supplying every other flag from the kind's own kindCases row and omitting
// one at a time is what makes each flag answer for itself. The rows are
// already maintained for TestKindRegistry_EveryKindRenders, so this reuses
// them rather than duplicating a second fixture table.
func TestKindRegistry_RequiredFlagsAreEnforced(t *testing.T) {
	groups := map[string][]summaryKind{
		"build":   buildKinds(),
		"publish": publishKinds(),
	}

	cases := kindCases()

	for group, kinds := range groups {
		for _, k := range kinds {
			key := group + " " + k.name

			tc, ok := cases[key]
			if !ok {
				continue // TestKindRegistry_EveryKindRenders reports the missing row.
			}

			for _, missing := range k.required {
				t.Run(key+" without --"+missing, func(t *testing.T) {
					ghaenv.Setup(t)

					argv := append([]string{group, k.name}, withoutFlag(tc.args, missing)...)

					err := runReport(t, argv...)
					if err == nil {
						t.Fatalf("kind %q without --%s: expected a required-flag error, got nil", key, missing)
					}

					if !strings.Contains(err.Error(), "--"+missing) {
						t.Errorf("kind %q without --%s: error %q does not name the missing flag", key, missing, err)
					}
				})
			}
		}
	}
}

// withoutFlag returns args with "--name" and its value removed. The kindCases
// rows are flag/value pairs, so dropping two entries removes exactly one flag.
func withoutFlag(args []string, name string) []string {
	out := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		if args[i] == "--"+name {
			i++ // skip the value too

			continue
		}

		out = append(out, args[i])
	}

	return out
}

// TestKindRegistry_HostileValuesCannotForgeRows is the escaping row the
// per-kind marker check does not give.
//
// Every kind renders operator-supplied flag values into a Markdown step
// summary, and that summary is what a reviewer reads to decide a release looks
// right. A value carrying a newline used to inject whole bullet rows: passing
// --version "1.0\n- **Signed:** yes" produced a summary claiming the build was
// signed. Three renderers -- maven, npm and gradle -- interpolated raw, while
// the rest already went through the summary escaping helpers; those three now
// do too.
//
// The canary is a value that would become a new row and a markup break if it
// reached the summary unescaped. What is asserted is the absence of the
// forged row, not a particular encoding, so the helpers stay free to change
// how they escape.
func TestKindRegistry_HostileValuesCannotForgeRows(t *testing.T) {
	const forged = "**Signed:** ✓ yes"

	hostile := "1.0\n- " + forged + "\n- **Reviewed:** yes"

	for _, tc := range []struct {
		key  string
		args []string
	}{
		{key: "build maven", args: []string{"--build-type", "library", "--group-id", "com.example", "--artifact-id", "app", "--version", hostile, "--java-version", "21"}},
		{key: "build npm", args: []string{"--package-name", "@org/app", "--version", hostile, "--node-version", "22"}},
		{key: "build gradle", args: []string{"--java-version", "21", "--tasks", "build", "--version", hostile}},
	} {
		t.Run(tc.key, func(t *testing.T) {
			env := ghaenv.Setup(t)

			argv := append(strings.Fields(tc.key), tc.args...)
			if err := runReport(t, argv...); err != nil {
				t.Fatalf("run %q: %v", tc.key, err)
			}

			body := env.Summary()

			// The forged row must not appear AS A ROW. The characters may well
			// survive inside the value -- escaping renders them inert rather
			// than deleting them -- so what is checked is a line that opens
			// with the summary's own bullet syntax and the injected label,
			// which is what a reader would see as a field the tool reported.
			for _, line := range strings.Split(body, "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "- **Signed:") {
					t.Errorf("a flag value forged a summary row:\n%s", body)

					break
				}
			}

			// And the summary is still one block: the injected newlines must
			// not have split it.
			if !strings.Contains(body, "Build Summary") {
				t.Errorf("the summary lost its heading:\n%s", body)
			}
		})
	}
}
