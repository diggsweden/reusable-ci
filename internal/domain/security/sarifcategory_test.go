// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"encoding/json"
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

	// Two matrix legs → two distinct analysis ids → no overwrite.
	if runID(t, image, 0) == runID(t, repo, 0) {
		t.Errorf("distinct categories produced the same automationDetails.id %q", runID(t, image, 0))
	}
}

func TestSetSARIFCategory_MultiRunGetsDistinctIDs(t *testing.T) {
	t.Parallel()

	out, err := security.SetSARIFCategory([]byte(`{"runs":[{"tool":{}},{"tool":{}}]}`), "cat")
	if err != nil {
		t.Fatal(err)
	}

	if a, b := runID(t, out, 0), runID(t, out, 1); a == b {
		t.Errorf("multi-run ids collided: %q == %q", a, b)
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
