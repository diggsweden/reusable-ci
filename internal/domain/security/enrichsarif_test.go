// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package security_test

import (
	"encoding/json"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/security"
)

func TestEnrichGitHubSARIF_AddsMatchBasedID(t *testing.T) {
	body := []byte(`{
  "runs": [{
    "results": [{
      "ruleId": "RULE",
      "message": {"text": "boom"},
      "locations": [{"physicalLocation":{"artifactLocation":{"uri":"a.go"},"region":{"startLine":12}}}],
      "fingerprints": {"matchBasedId/v1": "match-xyz"}
    }]
  }]
}`)

	got, err := security.EnrichGitHubSARIF(body)
	if err != nil {
		t.Fatal(err)
	}

	fp := firstPartialFingerprints(t, got)
	if fp["primaryLocationLineHash"] != "match-xyz" {
		t.Errorf("primaryLocationLineHash = %v, want match-xyz", fp["primaryLocationLineHash"])
	}
}

func TestEnrichGitHubSARIF_LeavesExistingHashAlone(t *testing.T) {
	body := []byte(`{
  "runs": [{
    "results": [{
      "ruleId": "RULE",
      "partialFingerprints": {"primaryLocationLineHash": "preserved"},
      "fingerprints": {"matchBasedId/v1": "match-xyz"}
    }]
  }]
}`)

	got, err := security.EnrichGitHubSARIF(body)
	if err != nil {
		t.Fatal(err)
	}

	fp := firstPartialFingerprints(t, got)
	if fp["primaryLocationLineHash"] != "preserved" {
		t.Errorf("got %v, want preserved", fp["primaryLocationLineHash"])
	}
}

func TestEnrichGitHubSARIF_FallbackCompositeHash(t *testing.T) {
	body := []byte(`{
  "runs": [{
    "results": [{
      "ruleId": "MY-RULE",
      "message": {"text": "missing semicolon"},
      "locations": [{"physicalLocation":{"artifactLocation":{"uri":"src/foo.go"},"region":{"startLine":42}}}]
    }]
  }]
}`)

	got, err := security.EnrichGitHubSARIF(body)
	if err != nil {
		t.Fatal(err)
	}

	fp := firstPartialFingerprints(t, got)

	want := "MY-RULE|src/foo.go|42|missing semicolon"
	if fp["primaryLocationLineHash"] != want {
		t.Errorf("hash = %q, want %q", fp["primaryLocationLineHash"], want)
	}
}

func TestEnrichGitHubSARIF_DefaultsOnMissingFields(t *testing.T) {
	body := []byte(`{
  "runs": [{
    "results": [{}]
  }]
}`)

	got, err := security.EnrichGitHubSARIF(body)
	if err != nil {
		t.Fatal(err)
	}

	fp := firstPartialFingerprints(t, got)
	if fp["primaryLocationLineHash"] != "rule|unknown|0|" {
		t.Errorf("got %q, want rule|unknown|0|", fp["primaryLocationLineHash"])
	}
}

func TestEnrichGitHubSARIF_EmptyExistingHashTreatedAsAbsent(t *testing.T) {
	body := []byte(`{
  "runs": [{
    "results": [{
      "ruleId": "X",
      "partialFingerprints": {"primaryLocationLineHash": ""},
      "fingerprints": {"matchBasedId/v1": "match"}
    }]
  }]
}`)

	got, err := security.EnrichGitHubSARIF(body)
	if err != nil {
		t.Fatal(err)
	}

	fp := firstPartialFingerprints(t, got)
	if fp["primaryLocationLineHash"] != "match" {
		t.Errorf("got %q, want match (empty hash should be replaced)", fp["primaryLocationLineHash"])
	}
}

func TestEnrichGitHubSARIF_PassesThroughNonObjectInputs(t *testing.T) {
	got, err := security.EnrichGitHubSARIF([]byte(`[1,2,3]`))
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != "[1,2,3]" {
		t.Errorf("got %q", got)
	}
}

func firstPartialFingerprints(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}

	runs, _ := doc["runs"].([]any)
	if len(runs) == 0 {
		t.Fatal("no runs")
	}

	run, ok := runs[0].(map[string]any)
	if !ok {
		t.Fatal("runs[0] is not an object")
	}

	results, _ := run["results"].([]any)
	if len(results) == 0 {
		t.Fatal("no results")
	}

	res, ok := results[0].(map[string]any)
	if !ok {
		t.Fatal("results[0] is not an object")
	}

	fp, _ := res["partialFingerprints"].(map[string]any)
	if fp == nil {
		t.Fatal("no partialFingerprints")
	}

	return fp
}
