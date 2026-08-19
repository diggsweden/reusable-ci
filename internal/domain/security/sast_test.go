// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
)

const sampleSARIF = `{
  "version": "2.1.0",
  "runs": [
    {
      "tool": {
        "driver": {
          "name": "opengrep",
          "semanticVersion": "1.2.3"
        }
      },
      "results": [
        {
          "ruleId": "go.lang.security.audit.dangerous-exec",
          "level": "error",
          "message": { "text": "Dangerous use of exec.\nReview the call." },
          "properties": { "security-severity": "8.5" },
          "locations": [
            {
              "physicalLocation": {
                "artifactLocation": { "uri": "cmd/main.go" },
                "region": { "startLine": 12, "endLine": 14 }
              }
            }
          ]
        },
        {
          "ruleId": "generic.secrets.hardcoded-token",
          "level": "warning",
          "message": { "text": "Possible hardcoded token." },
          "locations": [
            {
              "physicalLocation": {
                "artifactLocation": { "uri": "internal/cfg.go" },
                "region": { "startLine": 3 }
              }
            }
          ]
        }
      ]
    }
  ]
}`

func parseSARIF(t *testing.T, in string) map[string]any {
	t.Helper()

	var doc map[string]any
	if err := json.Unmarshal([]byte(in), &doc); err != nil {
		t.Fatalf("parse sarif: %v", err)
	}

	return doc
}

func TestSARIFToGitLabSAST_BasicShape(t *testing.T) {
	t.Parallel()
	gl := security.SARIFToGitLabSAST(parseSARIF(t, sampleSARIF), security.Options{
		Now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	})

	if gl.Version != security.GitLabSchemaVersion {
		t.Errorf("version = %q, want %q", gl.Version, security.GitLabSchemaVersion)
	}

	if gl.Scan.Type != "sast" {
		t.Errorf("scan.type = %q, want %q", gl.Scan.Type, "sast")
	}

	if gl.Scan.Scanner.ID != "opengrep" {
		t.Errorf("scanner.id = %q, want %q", gl.Scan.Scanner.ID, "opengrep")
	}

	if gl.Scan.Scanner.Version != "1.2.3" {
		t.Errorf("scanner.version = %q, want %q", gl.Scan.Scanner.Version, "1.2.3")
	}

	if got := len(gl.Vulnerabilities); got != 2 {
		t.Fatalf("vulnerabilities count = %d, want 2", got)
	}
}

func TestSARIFToGitLabSAST_SeverityFromCVSSThenLevel(t *testing.T) {
	t.Parallel()
	gl := security.SARIFToGitLabSAST(parseSARIF(t, sampleSARIF), security.Options{})

	// First finding: security-severity 8.5 → High (CVSS band wins).
	if got := gl.Vulnerabilities[0].Severity; got != "High" {
		t.Errorf("severities[0] = %q, want High", got)
	}
	// Second finding: no CVSS, level=warning → Medium.
	if got := gl.Vulnerabilities[1].Severity; got != "Medium" {
		t.Errorf("severities[1] = %q, want Medium", got)
	}
}

func TestSARIFToGitLabSAST_Location(t *testing.T) {
	t.Parallel()
	gl := security.SARIFToGitLabSAST(parseSARIF(t, sampleSARIF), security.Options{})

	loc := gl.Vulnerabilities[0].Location
	if loc.File != "cmd/main.go" {
		t.Errorf("location.file = %q, want %q", loc.File, "cmd/main.go")
	}

	if loc.StartLine != 12 || loc.EndLine != 14 {
		t.Errorf("location lines = %d-%d, want 12-14", loc.StartLine, loc.EndLine)
	}

	if loc.Dependency != nil {
		t.Errorf("SAST location should have nil dependency, got %+v", loc.Dependency)
	}

	if loc.Image != "" || loc.OperatingSystem != "" {
		t.Errorf("SAST location should not have image/os: %+v", loc)
	}
}

func TestSARIFToGitLabSAST_Identifier(t *testing.T) {
	t.Parallel()
	gl := security.SARIFToGitLabSAST(parseSARIF(t, sampleSARIF), security.Options{})

	ids := gl.Vulnerabilities[0].Identifiers
	if len(ids) != 1 {
		t.Fatalf("identifiers count = %d, want 1", len(ids))
	}

	if ids[0].Type != "opengrep_rule" {
		t.Errorf("identifier.type = %q, want %q", ids[0].Type, "opengrep_rule")
	}

	if ids[0].Value != "go.lang.security.audit.dangerous-exec" {
		t.Errorf("identifier.value = %q", ids[0].Value)
	}
}

func TestSARIFToGitLabSAST_NameFallsBackToFirstLine(t *testing.T) {
	t.Parallel()
	gl := security.SARIFToGitLabSAST(parseSARIF(t, `{
		"runs": [{"tool": {"driver": {"name": "x"}}, "results": [{
			"message": {"text": "First line.\nSecond line."},
			"locations": [{"physicalLocation": {
				"artifactLocation": {"uri": "a.go"}, "region": {"startLine": 1}
			}}]
		}]}]
	}`), security.Options{})

	if got := gl.Vulnerabilities[0].Name; got != "First line." {
		t.Errorf("name = %q, want %q (first line of message)", got, "First line.")
	}
}

func TestSARIFToGitLabSAST_DeterministicUUIDs(t *testing.T) {
	t.Parallel()
	doc := parseSARIF(t, sampleSARIF)
	first := security.SARIFToGitLabSAST(doc, security.Options{})
	second := security.SARIFToGitLabSAST(doc, security.Options{})

	seen := map[string]int{}

	for i := range first.Vulnerabilities {
		if first.Vulnerabilities[i].ID != second.Vulnerabilities[i].ID {
			t.Errorf("UUID for vuln %d not deterministic: %q vs %q",
				i, first.Vulnerabilities[i].ID, second.Vulnerabilities[i].ID)
		}

		// Two findings must not share an id, or GitLab collapses them
		// into one vulnerability. A constant id satisfies the
		// determinism check above perfectly.
		if prev, dup := seen[first.Vulnerabilities[i].ID]; dup {
			t.Errorf("vulns %d and %d share id %q", prev, i, first.Vulnerabilities[i].ID)
		}

		seen[first.Vulnerabilities[i].ID] = i
	}

	if len(first.Vulnerabilities) < 2 {
		t.Fatalf("fixture has %d findings; distinctness needs at least 2", len(first.Vulnerabilities))
	}
}

func TestSARIFToGitLabSAST_EmptyAndDefaults(t *testing.T) {
	t.Parallel()
	gl := security.SARIFToGitLabSAST(parseSARIF(t, `{"runs": []}`), security.Options{})

	if gl.Vulnerabilities == nil {
		t.Errorf("vulnerabilities should be empty array, not nil (JSON shape)")
	}

	if len(gl.Vulnerabilities) != 0 {
		t.Errorf("got %d vulns, want 0", len(gl.Vulnerabilities))
	}

	if gl.Scan.Scanner.ID != "sast" {
		t.Errorf("default scanner.id = %q, want %q", gl.Scan.Scanner.ID, "sast")
	}

	if gl.Scan.Scanner.Version != "unknown" {
		t.Errorf("default scanner.version = %q, want %q", gl.Scan.Scanner.Version, "unknown")
	}
}
