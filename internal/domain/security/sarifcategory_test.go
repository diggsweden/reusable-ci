// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
)

// runID parses a SARIF body and returns runs[i].automationDetails.id.
func runID(t *testing.T, body []byte, i int) string {
	t.Helper()

	var doc struct {
		Runs []struct {
			AutomationDetails struct {
				ID string `json:"id"`
			} `json:"automationDetails"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}

	if i >= len(doc.Runs) {
		t.Fatalf("run %d out of range (%d runs)", i, len(doc.Runs))
	}

	return doc.Runs[i].AutomationDetails.ID
}

func TestSetSARIFCategory_SingleRun(t *testing.T) {
	t.Parallel()

	out, err := security.SetSARIFCategory([]byte(`{"version":"2.1.0","runs":[{"tool":{}}]}`), "trivy-image")
	if err != nil {
		t.Fatal(err)
	}

	if got := runID(t, out, 0); got != "trivy-image" {
		t.Errorf("automationDetails.id = %q, want %q", got, "trivy-image")
	}
}

func TestSetSARIFCategory_DistinctCategoriesDoNotCollide(t *testing.T) {
	t.Parallel()

	body := []byte(`{"runs":[{"tool":{}}]}`)

	image, err := security.SetSARIFCategory(body, "scan-image")
	if err != nil {
		t.Fatal(err)
	}

	repo, err := security.SetSARIFCategory(body, "scan-repo")
	if err != nil {
		t.Fatal(err)
	}

	// Two matrix legs → two distinct analysis ids → no overwrite. By
	// value, not merely different: Code Scanning keys the analysis on
	// this string, so a consumer re-uploading under the same category
	// has to produce the same id, which inequality alone cannot show.
	if got := runID(t, image, 0); got != "scan-image" {
		t.Errorf("image id = %q, want scan-image", got)
	}

	if got := runID(t, repo, 0); got != "scan-repo" {
		t.Errorf("repo id = %q, want scan-repo", got)
	}

	// The input is shared between the two calls; neither may have
	// mutated it in place.
	if string(body) != `{"runs":[{"tool":{}}]}` {
		t.Errorf("input body was mutated: %s", body)
	}
}

func TestSetSARIFCategory_MultiRunGetsDistinctIDs(t *testing.T) {
	t.Parallel()

	out, err := security.SetSARIFCategory([]byte(`{"runs":[{"tool":{}},{"tool":{}}]}`), "cat")
	if err != nil {
		t.Fatal(err)
	}

	// The documented shape is <category>/<index>, and the shared prefix
	// is the point: it keeps both runs resolving to one category while
	// staying distinct analyses. Asserting only that they differ would
	// accept ids that dropped the prefix entirely.
	if got, want := runID(t, out, 0), "cat/0"; got != want {
		t.Errorf("run 0 id = %q, want %q", got, want)
	}

	if got, want := runID(t, out, 1), "cat/1"; got != want {
		t.Errorf("run 1 id = %q, want %q", got, want)
	}
}

// TestSetSARIFCategory_PreservesEverythingElse covers the claim that
// every other SARIF field survives. The runs here already declare their
// ids, so stamping has nothing to do and the document must come back
// semantically identical.
func TestSetSARIFCategory_PreservesEverythingElse(t *testing.T) {
	t.Parallel()

	body := []byte(`{
  "$schema": "https://json.schemastore.org/sarif-2.1.0.json",
  "version": "2.1.0",
  "runs": [{
    "tool": {"driver": {"name": "trivy", "rules": [{"id": "CVE-2024-1"}]}},
    "automationDetails": {"id": "already-set", "description": {"text": "keep me"}},
    "results": [{"ruleId": "CVE-2024-1", "message": {"text": "boom"}}]
  }]
}`)

	got, err := security.SetSARIFCategory(body, "override")
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

func TestSetSARIFCategory_RespectsPreexistingIDAndEmptyCategory(t *testing.T) {
	t.Parallel()

	withID := []byte(`{"runs":[{"automationDetails":{"id":"tool-set/"}}]}`)

	out, err := security.SetSARIFCategory(withID, "override")
	if err != nil {
		t.Fatal(err)
	}

	if got := runID(t, out, 0); got != "tool-set/" {
		t.Errorf("pre-existing id clobbered: got %q", got)
	}

	// Empty category is a no-op passthrough.
	same, err := security.SetSARIFCategory(withID, "")
	if err != nil {
		t.Fatal(err)
	}

	if string(same) != string(withID) {
		t.Errorf("empty category mutated body: %s", same)
	}
}
