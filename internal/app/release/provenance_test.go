// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestGenerateProvenance_CarriesIdentifiersAndDeps(t *testing.T) {
	t.Parallel()

	body, err := apprelease.GenerateProvenance(apprelease.ProvenanceInput{
		Checksums:     strings.NewReader(strings.Repeat("a", 64) + "  dist/app.tar.gz\n"),
		GoSum:         strings.NewReader("github.com/foo/bar v1.0.0 h1:xyz=\n"),
		RepositoryURL: "https://codeberg.org/itiquette/repo",
		Ref:           "v2.0.0",
		SHA:           strings.Repeat("d", 40),
		BuilderID:     "https://codeberg.org/itiquette/repo/release.yml@v2.0.0",
		InvocationID:  "https://codeberg.org/itiquette/repo/actions/runs/777",
		StartedOn:     "2026-06-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}

	got := decodeStatement(t, body)

	if id := got.Predicate.RunDetails.Builder.ID; id != "https://codeberg.org/itiquette/repo/release.yml@v2.0.0" {
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

// TestGenerateProvenance_RejectsMalformedSHA pins the fail-closed commit
// digest rule: the SHA lands in the attested resolvedDependencies, so junk
// must be refused here, not signed into evidence (callers no longer
// pre-validate in shell).
func TestGenerateProvenance_RejectsMalformedSHA(t *testing.T) {
	t.Parallel()

	for _, sha := range []string{"", "deadbeef", strings.Repeat("A", 40), strings.Repeat("g", 64), strings.Repeat("2", 41)} {
		_, err := apprelease.GenerateProvenance(apprelease.ProvenanceInput{
			Checksums:     strings.NewReader(strings.Repeat("b", 64) + "  x\n"),
			RepositoryURL: "https://git.example/o/r",
			Ref:           "v1",
			SHA:           sha,
			BuilderID:     "https://git.example/o/r/release.yml@v1",
			InvocationID:  "https://git.example/o/r/actions/runs/1",
			StartedOn:     "2026-06-01T00:00:00Z",
		})
		if !errors.Is(err, errs.ErrValidation) {
			t.Errorf("SHA %q: err = %v, want ErrValidation", sha, err)
		}
	}
}

// statement is a minimal view of the fields the tests assert on.
type statement struct {
	Predicate struct {
		BuildDefinition struct {
			BuildType            string                     `json:"buildType"`
			ExternalParameters   map[string]json.RawMessage `json:"externalParameters"`
			InternalParameters   map[string]string          `json:"internalParameters"`
			ResolvedDependencies []json.RawMessage          `json:"resolvedDependencies"`
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
		SHA:           strings.Repeat("a", 40),
		BuilderID:     "https://git.example/o/r/release.yml@v1",
		InvocationID:  "https://git.example/o/r/actions/runs/1",
		StartedOn:     "2026-06-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}

	if n := len(decodeStatement(t, body).Predicate.BuildDefinition.ResolvedDependencies); n != 1 {
		t.Errorf("got %d deps, want 1 (source only when no go.sum)", n)
	}
}

func TestGenerateProvenance_ForgejoActionsProfile(t *testing.T) {
	t.Parallel()

	body, err := apprelease.GenerateProvenance(apprelease.ProvenanceInput{
		Checksums:     strings.NewReader(strings.Repeat("a", 64) + "  app.tar.gz\n"),
		GoSum:         strings.NewReader("example.com/mod v0.1.0+incompatible h1:abc123=\n"),
		RepositoryURL: "https://codeberg.org/Itiquette/example",
		Ref:           "v1.2.3",
		SHA:           strings.Repeat("2", 40),
		BuilderID:     "https://codeberg.org/Itiquette/example/.forgejo/workflows/release.yml@v1.2.3",
		InvocationID:  "https://codeberg.org/Itiquette/example/actions/runs/123",
		StartedOn:     "2026-06-01T00:00:00Z",
		Profile:       apprelease.ProvenanceProfileForgejoActions,
		Workflow:      "release.yml",
	})
	if err != nil {
		t.Fatal(err)
	}

	got := decodeStatement(t, body)

	build := got.Predicate.BuildDefinition
	if build.BuildType != "https://forgejo.org/actions/buildtypes/workflow/v1" {
		t.Errorf("buildType = %q", build.BuildType)
	}

	if build.InternalParameters["runner"] != "forgejo-actions" {
		t.Errorf("runner internal parameter = %q", build.InternalParameters["runner"])
	}

	var workflow struct {
		Ref        string `json:"ref"`
		Repository string `json:"repository"`
		Path       string `json:"path"`
	}
	if err := json.Unmarshal(build.ExternalParameters["workflow"], &workflow); err != nil {
		t.Fatalf("externalParameters.workflow: %v", err)
	}

	if workflow.Ref != "v1.2.3" || workflow.Repository != "https://codeberg.org/Itiquette/example" || workflow.Path != ".forgejo/workflows/release.yml" {
		t.Errorf("workflow external parameters = %+v", workflow)
	}

	if _, ok := build.ExternalParameters["source"]; ok {
		t.Error("forgejo-actions profile must not emit generic externalParameters.source")
	}

	deps := build.ResolvedDependencies
	if len(deps) != 2 {
		t.Fatalf("got %d deps, want source + module", len(deps))
	}

	var sourceDep struct {
		URI    string            `json:"uri"`
		Digest map[string]string `json:"digest"`
	}
	if err := json.Unmarshal(deps[0], &sourceDep); err != nil {
		t.Fatalf("source dep: %v", err)
	}

	if sourceDep.URI != "git+https://codeberg.org/Itiquette/example@v1.2.3" || sourceDep.Digest["gitCommit"] != strings.Repeat("2", 40) {
		t.Errorf("source dep = %+v", sourceDep)
	}

	var moduleDep struct {
		URI    string            `json:"uri"`
		Digest map[string]string `json:"digest"`
	}
	if err := json.Unmarshal(deps[1], &moduleDep); err != nil {
		t.Fatalf("module dep: %v", err)
	}

	if moduleDep.URI != "pkg:golang/example.com/mod@v0.1.0%2Bincompatible" || moduleDep.Digest["gomod_h1"] != "abc123" {
		t.Errorf("module dep = %+v", moduleDep)
	}
}

func TestGenerateProvenance_ForgejoActionsProfileRequiresWorkflow(t *testing.T) {
	t.Parallel()

	_, err := apprelease.GenerateProvenance(apprelease.ProvenanceInput{
		Checksums:     strings.NewReader(strings.Repeat("a", 64) + "  app.tar.gz\n"),
		RepositoryURL: "https://codeberg.org/itiquette/example",
		Ref:           "v1.2.3",
		SHA:           strings.Repeat("2", 40),
		BuilderID:     "builder",
		StartedOn:     "2026-06-01T00:00:00Z",
		Profile:       apprelease.ProvenanceProfileForgejoActions,
	})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}
}

// TestGenerateProvenance_ExternalParameters pins the generic extras path
// on the release statement: caller-declared parameters land in
// buildDefinition.externalParameters, and any engine-computed key is
// reserved — a collision fails rather than overriding a fact.
func TestGenerateProvenance_ExternalParameters(t *testing.T) {
	t.Parallel()

	input := func(extras map[string]any) apprelease.ProvenanceInput {
		return apprelease.ProvenanceInput{
			Checksums:          strings.NewReader(strings.Repeat("a", 64) + "  app.tar.gz\n"),
			RepositoryURL:      "https://codeberg.org/itiquette/example",
			Ref:                "v1.2.3",
			SHA:                strings.Repeat("2", 40),
			BuilderID:          "https://codeberg.org/itiquette/example/release.yml@v1.2.3",
			InvocationID:       "https://codeberg.org/itiquette/example/actions/runs/1",
			StartedOn:          "2026-06-01T00:00:00Z",
			ExternalParameters: extras,
		}
	}

	t.Run("extras land in externalParameters", func(t *testing.T) {
		t.Parallel()

		body, err := apprelease.GenerateProvenance(input(map[string]any{"base_input_set": "abc123", "build_group": "core"}))
		if err != nil {
			t.Fatal(err)
		}

		ext := decodeStatement(t, body).Predicate.BuildDefinition.ExternalParameters
		if string(ext["base_input_set"]) != `"abc123"` || string(ext["build_group"]) != `"core"` {
			t.Errorf("extras missing from externalParameters: %#v", ext)
		}
	})

	t.Run("reserved-key collision fails", func(t *testing.T) {
		t.Parallel()

		_, err := apprelease.GenerateProvenance(input(map[string]any{"source": "shadowed"}))
		if !errors.Is(err, errs.ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
	})
}
