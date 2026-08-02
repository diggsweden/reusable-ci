// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package provenance builds a SLSA Provenance v1.0 predicate — and,
// optionally, the in-toto Statement v1 envelope around it — from typed,
// FORGE-NEUTRAL inputs. One shape serves both attestation paths:
//
//   - Predicate(...) → the bare predicate JSON that `cosign attest --type
//     slsaprovenance1` wraps, binding the subject (an OCI image) itself.
//     (cosign's bare "slsaprovenance" alias is SLSA v0.2; this is v1.0.)
//   - Build(...) + Statement.JSON() → the full statement (predicate +
//     explicit subjects) that `cosign sign-blob` signs for release blobs.
//
// The default vocabulary is deliberately forge-neutral: externalParameters
// carry {source, ref, image}, internalParameters are empty, and the builder id /
// invocation are plain URIs the caller derives from whatever CI env it runs
// in (GitHub/Forgejo GITHUB_*, GitLab CI_*). A Forgejo Actions release profile
// is available only to preserve forgejo-ci's shipped blob-provenance contract;
// the generic profile remains the default for new consumers.
//
// The package is pure: no env, git, I/O, or signing — callers resolve every
// value and hand it in, so the predicate is golden-testable in isolation.
//
// References:
//   - https://slsa.dev/spec/v1.0/provenance
//   - https://github.com/in-toto/attestation/blob/main/spec/v1/statement.md
package provenance

import (
	"encoding/json"
	"fmt"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// Forge-neutral SLSA buildType URIs. A buildType names the build PROCESS,
// not the forge, so the same value is emitted on GitHub / Forgejo / GitLab.
// Versioned so the predicate's interpretation stays stable.
const (
	ContainerBuildType = "https://diggsweden.github.io/reusable-ci/container-build/v1"
	ReleaseBuildType   = "https://diggsweden.github.io/reusable-ci/release-build/v1"

	// ForgejoActionsWorkflowBuildType preserves forgejo-ci's shipped release
	// blob provenance contract while the generic reusable-ci profile remains the
	// default for GitHub/GitLab and new consumers.
	ForgejoActionsWorkflowBuildType = "https://forgejo.org/actions/buildtypes/workflow/v1"
)

// Subject is one artifact a statement attests: a name and its SHA-256 hex.
type Subject struct {
	Name   string
	SHA256 string
}

// Dependency is one resolvedDependencies entry: a package URI and a single
// named digest (e.g. "gitCommit" for the source, "gomod_h1" for a Go module,
// "sha256" for a base image). Annotations carry optional, forge-neutral
// metadata about the input (e.g. role="base-image" for the base a container
// was built FROM) — the SLSA-standard home for base-image lineage, so no
// bespoke predicate type is needed.
type Dependency struct {
	URI         string
	DigestType  string
	Digest      string
	Annotations map[string]string
}

// WorkflowExternalParameters is forgejo-ci's historical release provenance
// externalParameters.workflow object. Keep it explicit so the generic profile
// can stay forge-neutral by default.
type WorkflowExternalParameters struct {
	Ref        string
	Repository string
	Path       string
}

// Input is everything the builder needs; all values are pre-resolved.
type Input struct {
	// Subjects are the attested artifacts. Required by Build (statement);
	// ignored by Predicate (cosign binds the image subject itself).
	Subjects []Subject

	// BuildType identifies the build process (use ContainerBuildType /
	// ReleaseBuildType). Required.
	BuildType string

	// BuilderID is the build identity URI, derived forge-neutrally by the
	// caller (e.g. <server>/<repo>/<workflow-or-job>@<ref>). Required.
	BuilderID string

	// SourceURI is the versioned source, e.g. git+https://github.com/org/repo.
	// Emitted as externalParameters.source. Required.
	SourceURI string

	// Ref is the triggering ref, e.g. refs/tags/v1.2.3 (optional).
	Ref string

	// ImageName is the built image (optional; externalParameters.image).
	ImageName string

	// Flavor is the build variant (e.g. a ci-builder flavor like "rust").
	// Optional; emitted as externalParameters.flavor.
	Flavor string

	// BaseInputID is a content identifier (sha256 hex) for this build's base
	// inputs — the standard, forge-neutral home for forgejo-ci's per-flavor
	// base_input_id. Optional; emitted as externalParameters.base_input_id.
	BaseInputID string

	// Workflow optionally replaces the generic {source, ref} external
	// parameters with forgejo-ci's legacy workflow object for release blobs.
	Workflow *WorkflowExternalParameters

	// InternalParameters carries profile-specific stable metadata. Generic
	// reusable-ci provenance leaves this empty.
	InternalParameters map[string]string

	// InvocationID is the unique run identifier (run/job URL).
	InvocationID string

	// StartedOn / FinishedOn are RFC3339 UTC; commit-derived for repro.
	StartedOn  string
	FinishedOn string

	// ResolvedDeps are the build's resolved inputs — at minimum the source
	// commit (see SourceDependency), plus e.g. Go modules. Caller-supplied.
	ResolvedDeps []Dependency
}

type resourceDescriptor struct {
	URI         string            `json:"uri,omitempty"`
	Digest      map[string]string `json:"digest,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type buildDefinitionJSON struct {
	BuildType            string               `json:"buildType"`
	ExternalParameters   map[string]any       `json:"externalParameters"`
	InternalParameters   map[string]string    `json:"internalParameters"`
	ResolvedDependencies []resourceDescriptor `json:"resolvedDependencies"`
}

type workflowExternalJSON struct {
	Ref        string `json:"ref"`
	Repository string `json:"repository"`
	Path       string `json:"path"`
}

type builderJSON struct {
	ID string `json:"id"`
}

type metadataJSON struct {
	InvocationID string `json:"invocationId,omitempty"`
	StartedOn    string `json:"startedOn,omitempty"`
	FinishedOn   string `json:"finishedOn,omitempty"`
}

type runDetailsJSON struct {
	Builder  builderJSON  `json:"builder"`
	Metadata metadataJSON `json:"metadata"`
}

type predicateJSON struct {
	BuildDefinition buildDefinitionJSON `json:"buildDefinition"`
	RunDetails      runDetailsJSON      `json:"runDetails"`
}

type subjectJSON struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}

// Statement is the in-toto Statement v1 envelope.
type Statement struct {
	Type          string        `json:"_type"`
	Subject       []subjectJSON `json:"subject"`
	PredicateType string        `json:"predicateType"`
	Predicate     predicateJSON `json:"predicate"`
}

const (
	statementType = "https://in-toto.io/Statement/v1"
	predicateType = "https://slsa.dev/provenance/v1"
)

// validate checks the identifying fields every predicate needs, so a
// provenance with missing context is refused rather than silently emitted.
func (in Input) validate() error {
	for _, miss := range []struct {
		name string
		val  string
	}{
		{"BuildType", in.BuildType},
		{"BuilderID", in.BuilderID},
		{"SourceURI", in.SourceURI},
	} {
		if miss.val == "" {
			return fmt.Errorf("provenance: %s is required: %w", miss.name, errs.ErrUsage)
		}
	}

	if in.Workflow != nil {
		for _, miss := range []struct {
			name string
			val  string
		}{
			{"Workflow.Ref", in.Workflow.Ref},
			{"Workflow.Repository", in.Workflow.Repository},
			{"Workflow.Path", in.Workflow.Path},
		} {
			if miss.val == "" {
				return fmt.Errorf("provenance: %s is required: %w", miss.name, errs.ErrUsage)
			}
		}
	}

	return nil
}

func buildPredicate(in Input) predicateJSON {
	ext := map[string]any{}
	if in.Workflow != nil {
		ext["workflow"] = workflowExternalJSON{
			Ref:        in.Workflow.Ref,
			Repository: in.Workflow.Repository,
			Path:       in.Workflow.Path,
		}
	} else {
		ext["source"] = in.SourceURI
		if in.Ref != "" {
			ext["ref"] = in.Ref
		}
	}

	if in.ImageName != "" {
		ext["image"] = in.ImageName
	}

	if in.Flavor != "" {
		ext["flavor"] = in.Flavor
	}

	if in.BaseInputID != "" {
		ext["base_input_id"] = in.BaseInputID
	}

	deps := make([]resourceDescriptor, 0, len(in.ResolvedDeps))
	for _, d := range in.ResolvedDeps {
		deps = append(deps, resourceDescriptor{
			URI:         d.URI,
			Digest:      map[string]string{d.DigestType: d.Digest},
			Annotations: d.Annotations,
		})
	}

	internal := map[string]string{}

	for key, value := range in.InternalParameters {
		if value != "" {
			internal[key] = value
		}
	}

	return predicateJSON{
		BuildDefinition: buildDefinitionJSON{
			BuildType:            in.BuildType,
			ExternalParameters:   ext,
			InternalParameters:   internal,
			ResolvedDependencies: deps,
		},
		RunDetails: runDetailsJSON{
			Builder: builderJSON{ID: in.BuilderID},
			Metadata: metadataJSON{
				InvocationID: in.InvocationID,
				StartedOn:    in.StartedOn,
				FinishedOn:   in.FinishedOn,
			},
		},
	}
}

// Predicate renders just the SLSA Provenance v1.0 predicate JSON (no in-toto
// Statement envelope, no subjects) — for `cosign attest`, which adds the
// subject (the image digest) itself.
func Predicate(in Input) ([]byte, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}

	body, err := json.MarshalIndent(buildPredicate(in), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("provenance: marshal predicate: %w", err)
	}

	return append(body, '\n'), nil
}

// Build assembles the in-toto Statement (predicate + explicit subjects) for
// blob signing (cosign sign-blob). Requires at least one subject.
func Build(in Input) (Statement, error) {
	if len(in.Subjects) == 0 {
		return Statement{}, fmt.Errorf("provenance: no subjects: %w", errs.ErrUsage)
	}

	if err := in.validate(); err != nil {
		return Statement{}, err
	}

	subjects := make([]subjectJSON, 0, len(in.Subjects))
	for _, s := range in.Subjects {
		subjects = append(subjects, subjectJSON{Name: s.Name, Digest: map[string]string{"sha256": s.SHA256}})
	}

	return Statement{
		Type:          statementType,
		Subject:       subjects,
		PredicateType: predicateType,
		Predicate:     buildPredicate(in),
	}, nil
}

// JSON renders the statement as indented JSON with a trailing newline.
func (s Statement) JSON() ([]byte, error) {
	body, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("provenance: marshal statement: %w", err)
	}

	return append(body, '\n'), nil
}
