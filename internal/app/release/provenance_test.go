// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"encoding/json"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/internal/app/release"
	"github.com/diggsweden/reusable-ci/internal/domain/provenance"
)

func TestGenerateProvenance_DerivesIdentifiers(t *testing.T) {
	t.Parallel()

	body, err := apprelease.GenerateProvenance(apprelease.ProvenanceInput{
		Checksums:     strings.NewReader(strings.Repeat("a", 64) + "  dist/app.tar.gz\n"),
		GoSum:         strings.NewReader("github.com/foo/bar v1.0.0 h1:xyz=\n"),
		RepositoryURL: "https://codeberg.org/itiquette/repo",
		Ref:           "v2.0.0",
		SHA:           "deadbeef",
		WorkflowFile:  "release.yml",
		RunID:         "777",
		StartedOn:     "2026-06-01T00:00:00Z",
		Profile: provenance.Profile{
			BuildType:         "https://forgejo.org/actions/buildtypes/workflow/v1",
			WorkflowDirPrefix: ".forgejo/workflows/",
			RunnerLabel:       "forgejo-actions",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := decodeStatement(t, body)

	// Spot-check the derived builder + invocation identifiers.
	if id := got.Predicate.RunDetails.Builder.ID; id != "https://codeberg.org/itiquette/repo/.forgejo/workflows/release.yml@v2.0.0" {
		t.Errorf("builder id = %q", id)
	}

	if id := got.Predicate.RunDetails.Metadata.InvocationID; id != "https://codeberg.org/itiquette/repo/actions/runs/777" {
		t.Errorf("invocation id = %q", id)
	}

	// Source dep first, then the parsed go.sum module.
	if n := len(got.Predicate.BuildDefinition.ResolvedDependencies); n != 2 {
		t.Fatalf("got %d resolved deps, want 2 (source + 1 module)", n)
	}
}

// statement is a minimal view of the fields the tests assert on.
type statement struct {
	Predicate struct {
		BuildDefinition struct {
			ResolvedDependencies []json.RawMessage `json:"resolvedDependencies"`
		} `json:"buildDefinition"`
		RunDetails struct {
			Builder struct {
				ID string `json:"id"`
			} `json:"builder"`
			Metadata struct {
				InvocationID string `json:"invocationId"`
			} `json:"metadata"`
		} `json:"runDetails"`
	} `json:"predicate"`
}

func decodeStatement(t *testing.T, body []byte) statement {
	t.Helper()

	var s statement
	if err := json.Unmarshal(body, &s); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	return s
}

func TestGenerateProvenance_NoGoSum(t *testing.T) {
	t.Parallel()

	body, err := apprelease.GenerateProvenance(apprelease.ProvenanceInput{
		Checksums:     strings.NewReader(strings.Repeat("b", 64) + "  x\n"),
		GoSum:         nil, // optional
		RepositoryURL: "https://git.example/o/r",
		Ref:           "v1",
		SHA:           "abc",
		WorkflowFile:  "release.yml",
		RunID:         "1",
		StartedOn:     "2026-06-01T00:00:00Z",
		Profile:       provenance.Profile{BuildType: "bt", WorkflowDirPrefix: ".github/workflows/", RunnerLabel: "github-actions"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if n := len(decodeStatement(t, body).Predicate.BuildDefinition.ResolvedDependencies); n != 1 {
		t.Errorf("got %d deps, want 1 (source only when no go.sum)", n)
	}
}
