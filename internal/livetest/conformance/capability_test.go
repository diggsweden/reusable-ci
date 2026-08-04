// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package conformance_test

// PAR-CAP-1 carries no build tag on purpose.
//
// `Capabilities()` is derived from role membership, so a matrix that claims a
// feature the gates refuse is already impossible. What is still possible — and
// what happened — is the published table falling *behind* the model: today
// `Capabilities` carries five fields and docs/providers.md lists four, so a
// reader cannot discover from the docs that run artifacts work on Forgejo and
// not GitLab. This test closes that gap and keeps it closed, and it needs no
// forge to do it, so it runs on every commit.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

// capabilityRows maps a published row label to the field it reports. The
// mapping is spelled out rather than derived: a row label is prose written for
// an adopter, and deciding which model field it stands for is exactly the
// editorial judgement a test should pin rather than guess.
func capabilityRows() map[string]func(provider.Capabilities) bool {
	return map[string]func(provider.Capabilities) bool{
		"SARIF / Code Scanning upload":          func(c provider.Capabilities) bool { return c.SARIFUpload },
		"SLSA build-provenance attestation API": func(c provider.Capabilities) bool { return c.Attestation },
		"Keyless OIDC signing":                  func(c provider.Capabilities) bool { return c.KeylessOIDC },
		"Release asset upload":                  func(c provider.Capabilities) bool { return c.ReleaseAssets },
		"Run artifact upload/download":          func(c provider.Capabilities) bool { return c.RunArtifacts },
	}
}

func TestCapabilityMatrix_MatchesTheAdapters(t *testing.T) {
	t.Parallel()

	table := readCapabilityTable(t)
	rows := capabilityRows()

	// Every modelled capability must be published. This is the direction that
	// actually failed: the model grew a field and the table did not.
	if len(table) != len(rows) {
		t.Errorf("docs/providers.md publishes %d capability rows, the model has %d — every capability must be documented",
			len(table), len(rows))
	}

	for label, report := range rows {
		published, ok := table[label]
		if !ok {
			t.Errorf("capability %q is modelled but missing from the docs/providers.md matrix", label)

			continue
		}

		for _, kind := range livetest.Platforms() {
			capabilities, known := livetest.Capabilities(kind)
			if !known {
				t.Fatalf("no adapter for platform %q", kind)
			}

			want := report(capabilities)

			got, listed := published[string(kind)]
			if !listed {
				t.Errorf("capability %q does not list a value for %s", label, kind)

				continue
			}

			if got != want {
				t.Errorf("capability %q for %s: docs say %v, the adapter reports %v",
					label, kind, got, want)
			}
		}
	}
}

// readCapabilityTable parses the published matrix into label → forge → claim.
func readCapabilityTable(t *testing.T) map[string]map[string]bool {
	t.Helper()

	path := filepath.Join("..", "..", "..", "docs", "providers.md")

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var (
		header []string
		table  = map[string]map[string]bool{}
		row    = regexp.MustCompile(`^\|(.+)\|$`)
	)

	for _, line := range strings.Split(string(content), "\n") {
		match := row.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}

		cells := splitRow(match[1])

		if strings.EqualFold(cells[0], "Capability") {
			header = cells

			continue
		}

		if header == nil || len(cells) != len(header) || strings.HasPrefix(cells[0], "---") {
			continue
		}

		claims := make(map[string]bool, len(cells)-1)

		for i := 1; i < len(cells); i++ {
			// The table marks a claim with ✅ and its absence with ❌; anything
			// else is a cell a human has to look at, so refuse to guess.
			switch cells[i] {
			case "✅":
				claims[header[i]] = true
			case "❌":
				claims[header[i]] = false
			default:
				t.Fatalf("capability row %q has an unreadable cell %q under %q", cells[0], cells[i], header[i])
			}
		}

		table[cells[0]] = claims
	}

	if len(table) == 0 {
		t.Fatalf("found no capability matrix in %s", path)
	}

	return table
}

func splitRow(line string) []string {
	parts := strings.Split(line, "|")
	cells := make([]string, 0, len(parts))

	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}

	return cells
}
