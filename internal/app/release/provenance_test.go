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

	// externalParameters.source is where a verifier reads which repository
	// the artifact claims to come from, and it is composed here rather than
	// in the domain. Nothing asserted it: the whole suite passed with this
	// hardcoded to a foreign repository.
	ext := got.Predicate.BuildDefinition.ExternalParameters
	if src, ref := string(ext["source"]), string(ext["ref"]); src != `"git+https://codeberg.org/itiquette/repo"` || ref != `"v2.0.0"` {
		t.Errorf("externalParameters source = %s, ref = %s", src, ref)
	}

	// Source dep first, then the parsed go.sum module. A count alone would
	// not notice the commit digest going missing or the ref dropping out of
	// the source URI, and this statement is what a verifier trusts.
	deps := got.Predicate.BuildDefinition.ResolvedDependencies
	if len(deps) != 2 {
		t.Fatalf("got %d resolved deps, want 2 (source + 1 module)", len(deps))
	}

	if source := decodeDependency(t, deps[0]); source.URI != "git+https://codeberg.org/itiquette/repo@v2.0.0" || source.Digest["gitCommit"] != strings.Repeat("d", 40) {
		t.Errorf("source dep = %+v", source)
	}

	// That the module carries through at all is the claim here. How a
	// go.sum line becomes a purl is provenance.ParseGoSum's rule and is
	// pinned by TestParseGoSum in internal/domain/provenance.
	if module := decodeDependency(t, deps[1]); module.URI != "pkg:golang/github.com/foo/bar@v1.0.0" || module.Digest["gomod_h1"] != "xyz" {
		t.Errorf("module dep = %+v", module)
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

// dependency is a minimal view of one resolvedDependencies entry.
type dependency struct {
	URI    string            `json:"uri"`
	Digest map[string]string `json:"digest"`
}

func decodeDependency(t *testing.T, raw json.RawMessage) dependency {
	t.Helper()

	var dep dependency
	if err := json.Unmarshal(raw, &dep); err != nil {
		t.Fatalf("resolved dependency is not valid JSON: %v", err)
	}

	return dep
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

// TestGenerateProvenance_ForgejoProfileNamesTheWorkflowNotAGenericSource pins
// what the forgejo-actions profile changes: the build is described as a
// workflow run — its own buildType, runner and workflow parameters — and the
// generic externalParameters.source is deliberately absent, because under this
// profile the workflow parameters carry that identity instead.
//
// What the profile must not change is the dependency list, so the count is
// asserted here. What the entries look like is the release statement's own
// behaviour and is pinned by TestGenerateProvenance_CarriesIdentifiersAndDeps.
func TestGenerateProvenance_ForgejoProfileNamesTheWorkflowNotAGenericSource(t *testing.T) {
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

	// Naming the build after the workflow must not cost the attestation its
	// dependencies: the source commit and the go.sum module are still there.
	if n := len(build.ResolvedDependencies); n != 2 {
		t.Errorf("got %d deps, want source + module", n)
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
