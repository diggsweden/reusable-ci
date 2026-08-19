// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
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

// TestEnrichGitHubSARIF_PreservesEverythingElse covers the reason the
// function decodes into `any` at all: per its doc comment it "preserves
// the SARIF document's other fields untouched". Every other test reads
// only runs[0].results[0].partialFingerprints, so a rewrite that dropped
// the tool driver, the rule metadata or the schema would pass them all
// -- and Code Scanning renders alerts from exactly those fields.
//
// The input already carries a hash on every result, so enrichment has
// nothing to add and the document must come back semantically identical.
func TestEnrichGitHubSARIF_PreservesEverythingElse(t *testing.T) {
	t.Parallel()

	body := []byte(`{
  "$schema": "https://json.schemastore.org/sarif-2.1.0.json",
  "version": "2.1.0",
  "runs": [{
    "tool": {"driver": {
      "name": "opengrep",
      "semanticVersion": "1.2.3",
      "rules": [{"id": "RULE", "help": {"text": "do not do that"}, "properties": {"tags": ["security"]}}]
    }},
    "invocations": [{"executionSuccessful": true}],
    "results": [{
      "ruleId": "RULE",
      "level": "error",
      "message": {"text": "boom"},
      "locations": [{"physicalLocation":{"artifactLocation":{"uri":"a.go"},"region":{"startLine":12,"snippet":{"text":"x := 1"}}}}],
      "partialFingerprints": {"primaryLocationLineHash": "kept"},
      "properties": {"confidence": "HIGH"}
    }]
  }]
}`)

	got, err := security.EnrichGitHubSARIF(body)
	if err != nil {
		t.Fatal(err)
	}

	var before, after any
	if err := json.Unmarshal(body, &before); err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal(got, &after); err != nil {
		t.Fatalf("output is not valid SARIF JSON: %v", err)
	}

	if !reflect.DeepEqual(before, after) {
		t.Errorf("document changed\nbefore: %#v\nafter:  %#v", before, after)
	}
}

// TestEnrichGitHubSARIF_EnrichesEveryResultInEveryRun covers the two
// loops. A document from a multi-tool scan has several runs, and any
// result left without a fingerprint gets a fresh alert ID on every push
// -- the exact problem this function exists to prevent. Stopping after
// the first result, or the first run, satisfied every other test here.
func TestEnrichGitHubSARIF_EnrichesEveryResultInEveryRun(t *testing.T) {
	t.Parallel()

	body := []byte(`{
  "runs": [
    {"results": [
      {"ruleId": "A", "message": {"text": "first"}, "locations": [{"physicalLocation":{"artifactLocation":{"uri":"a.go"},"region":{"startLine":1}}}]},
      {"ruleId": "B", "message": {"text": "second"}, "locations": [{"physicalLocation":{"artifactLocation":{"uri":"b.go"},"region":{"startLine":2}}}]}
    ]},
    {"results": [
      {"ruleId": "C", "message": {"text": "third"}, "locations": [{"physicalLocation":{"artifactLocation":{"uri":"c.go"},"region":{"startLine":3}}}]}
    ]}
  ]
}`)

	got, err := security.EnrichGitHubSARIF(body)
	if err != nil {
		t.Fatal(err)
	}

	var doc struct {
		Runs []struct {
			Results []struct {
				PartialFingerprints map[string]string `json:"partialFingerprints"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatal(err)
	}

	want := [][]string{
		{"A|a.go|1|first", "B|b.go|2|second"},
		{"C|c.go|3|third"},
	}

	if len(doc.Runs) != len(want) {
		t.Fatalf("runs = %d, want %d", len(doc.Runs), len(want))
	}

	for i, wantRun := range want {
		if len(doc.Runs[i].Results) != len(wantRun) {
			t.Fatalf("run %d results = %d, want %d", i, len(doc.Runs[i].Results), len(wantRun))
		}

		for j, wantHash := range wantRun {
			if got := doc.Runs[i].Results[j].PartialFingerprints["primaryLocationLineHash"]; got != wantHash {
				t.Errorf("run %d result %d hash = %q, want %q", i, j, got, wantHash)
			}
		}
	}
}
